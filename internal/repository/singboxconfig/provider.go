package singboxconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Provider struct {
	actualConfigPath string
	tempConfigPath   string
}

// Group представляет информацию о группе для синхронизации.
type Group struct {
	Name            string
	Description     string
	DefaultOutbound string

	// DNSServer — тег DNS-сервера для доменов и IP группы. Пустое значение — DNS-правила группы не нужны.
	DNSServer string
}

func NewProvider(actualConfigPath string) *Provider {
	tempDir := os.TempDir()
	tempConfigPath := filepath.Join(tempDir, "sing-box-config-temp.json")

	return &Provider{
		actualConfigPath: actualConfigPath,
		tempConfigPath:   tempConfigPath,
	}
}

// GetActualConfig читает и возвращает текущий конфиг sing-box (как есть).
func (p *Provider) GetActualConfig() ([]byte, error) {
	configData, err := os.ReadFile(p.actualConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}

		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	return configData, nil
}

// HasPending возвращает true, если есть несохраненный конфиг (временный конфиг).
func (p *Provider) HasPending() bool {
	if _, err := os.Stat(p.tempConfigPath); err == nil {
		return true
	}

	return false
}

// GetTempConfig возвращает временный конфиг, если он существует.
func (p *Provider) GetTempConfig() ([]byte, error) {
	configData, err := os.ReadFile(p.tempConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}

		return nil, fmt.Errorf("cannot read temp config: %w", err)
	}

	return configData, nil
}

// GetTempOrActualConfig возвращает временный конфиг, либо основной, если временного нет.
func (p *Provider) GetTempOrActualConfig() ([]byte, error) {
	configData, err := p.GetTempConfig()
	if err != nil && os.IsNotExist(err) {
		return p.GetActualConfig()
	}

	return configData, err
}

// GetTempOrActualConfigParsed возвращает временный конфиг, либо основной, если временного нет,
// распарсенным в map. Комментарии удаляются перед парсингом.
func (p *Provider) GetTempOrActualConfigParsed() (map[string]any, error) {
	configData, err := p.GetTempOrActualConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot get config: %w", err)
	}

	var config map[string]any

	if err := json.Unmarshal(removeComments(configData), &config); err != nil {
		return nil, fmt.Errorf("cannot parse config json: %w", err)
	}

	return config, nil
}

// GetActualConfigParsed возвращает основной конфиг, распарсенный в map. Комментарии удаляются.
func (p *Provider) GetActualConfigParsed() (map[string]any, error) {
	configData, err := p.GetActualConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot get config: %w", err)
	}

	var config map[string]any

	if err = json.Unmarshal(removeComments(configData), &config); err != nil {
		return nil, fmt.Errorf("cannot parse config json: %w", err)
	}

	return config, nil
}

// WriteActualConfig записывает основной конфиг. Файл пишется in-place, чтобы не ломать bind mount.
func (p *Provider) WriteActualConfig(config map[string]any) error {
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err = os.WriteFile(p.actualConfigPath, append(configData, '\n'), 0644); err != nil {
		return fmt.Errorf("cannot write actual config: %w", err)
	}

	return nil
}

// SaveTempConfig сохраняет конфиг во временный файл без применения.
func (p *Provider) SaveTempConfig(configData []byte) error {
	// Валидируем JSON (с комментариями).
	if err := validateJSONWithComments(configData); err != nil {
		return fmt.Errorf("cannot parse json: %w", err)
	}

	if err := os.WriteFile(p.tempConfigPath, configData, 0644); err != nil {
		return fmt.Errorf("cannot write temp config: %w", err)
	}

	return nil
}

// ApplyTempConfigToActual применяет временный конфиг к основному файлу конфигурации.
func (p *Provider) ApplyTempConfigToActual() error {
	content, err := p.GetTempConfig()
	if err != nil {
		return fmt.Errorf("cannot get temp config: %w", err)
	}

	// Валидируем JSON (с комментариями).
	if err := validateJSONWithComments(content); err != nil {
		return fmt.Errorf("cannot validate temp config json: %w", err)
	}

	// TODO: Сохранять историю изменений.

	if err = os.WriteFile(p.actualConfigPath, content, 0644); err != nil {
		return fmt.Errorf("cannot write actual config: %w", err)
	}

	_ = p.RemoveTempConfig()

	return nil
}

// RemoveTempConfig удаляет временный конфиг.
func (p *Provider) RemoveTempConfig() error {
	if err := os.Remove(p.tempConfigPath); err != nil {
		return fmt.Errorf("cannot remove temp config: %w", err)
	}

	return nil
}

// GetTempPath возвращает путь к временному конфигу.
func (p *Provider) GetTempPath() string {
	return p.tempConfigPath
}

// GetActualPath возвращает путь к основному конфигу.
func (p *Provider) GetActualPath() string {
	return p.actualConfigPath
}

// SyncGroupsToConfig синхронизирует все группы в конфиг sing-box по пути configPath.
// Функция идемпотентна: повторный вызов не создает дубликатов rule-set-ов, правил и selector-ов.
func (p *Provider) SyncGroupsToConfig(configPath string, groups []Group) error {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cannot read config: %w", err)
	}

	var config map[string]any

	if err = json.Unmarshal(removeComments(configData), &config); err != nil {
		return fmt.Errorf("cannot parse config: %w", err)
	}

	if err = syncGroups(config, groups, groups); err != nil {
		return fmt.Errorf("cannot sync groups: %w", err)
	}

	if route, ok := config["route"].(map[string]any); ok {
		ensureServiceRules(route)
	}

	// Исключения синхронизируются последними: их правило должно стоять перед служебными.
	if err = EnsureBypass(config); err != nil {
		return fmt.Errorf("cannot sync bypass: %w", err)
	}

	configData, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err = os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	return nil
}

// ensureServiceRules добавляет в начало route.rules отсутствующие служебные правила
// (sniff, hijack-dns, resolve, ip_is_private -> direct).
func ensureServiceRules(route map[string]any) {
	rules, _ := route["rules"].([]any)

	var (
		foundSniff     bool
		foundHijackDNS bool
		foundResolve   bool
		foundPrivate   bool
	)

	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]any)
		if !ok {
			continue
		}

		switch action, _ := ruleMap["action"].(string); action {
		case "sniff":
			foundSniff = true

		case "hijack-dns":
			foundHijackDNS = true

		case "resolve":
			foundResolve = true
		}

		if _, ok := ruleMap["ip_is_private"]; ok {
			foundPrivate = true
		}
	}

	serviceRules := make([]any, 0, 4)

	if !foundSniff {
		serviceRules = append(serviceRules, map[string]any{
			"action": "sniff",
		})
	}

	if !foundHijackDNS {
		serviceRules = append(serviceRules, map[string]any{
			"action":   "hijack-dns",
			"protocol": "dns",
		})
	}

	if !foundResolve {
		serviceRules = append(serviceRules, map[string]any{
			"action":   "resolve",
			"strategy": "ipv4_only",
		})
	}

	if !foundPrivate {
		serviceRules = append(serviceRules, map[string]any{
			"ip_is_private": true,
			"outbound":      "direct",
		})
	}

	if len(serviceRules) == 0 {
		return
	}

	route["rules"] = append(serviceRules, rules...)
}

// validateJSONWithComments валидирует JSON с поддержкой комментариев.
func validateJSONWithComments(configData []byte) error {
	cleanedConfigData := removeComments(configData)

	var tmp interface{}

	if err := json.Unmarshal(cleanedConfigData, &tmp); err != nil {
		return fmt.Errorf("cannot unmarshal json: %w", err)
	}

	return nil
}

// removeComments удаляет все типы комментариев из JSON.
func removeComments(data []byte) []byte {
	text := string(data)

	// Удаляем многострочные комментарии /* */.
	multilineCommentRe := regexp.MustCompile(`(?s)/\*.*?\*/`)
	text = multilineCommentRe.ReplaceAllString(text, "")

	// Удаляем однострочные комментарии // и #.
	lines := strings.Split(text, "\n")
	var cleanedLines []string

	for _, line := range lines {
		// Проверяем, не внутри ли комментарий строки.
		inString := false
		escaped := false
		commentStart := -1

		for i := 0; i < len(line); i++ {
			char := line[i]

			if escaped {
				escaped = false

				continue
			}

			if char == '\\' {
				escaped = true

				continue
			}

			if char == '"' {
				inString = !inString

				continue
			}

			if !inString {
				if i < len(line)-1 && line[i:i+2] == "//" {
					commentStart = i

					break
				}

				if char == '#' {
					commentStart = i

					break
				}
			}
		}

		if commentStart >= 0 {
			line = line[:commentStart]
		}

		cleanedLines = append(cleanedLines, line)
	}

	return []byte(strings.Join(cleanedLines, "\n"))
}
