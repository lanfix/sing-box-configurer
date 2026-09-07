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

// SyncGroupsToConfig синхронизирует группы в конфиг sing-box.
func (p *Provider) SyncGroupsToConfig(configPath string, groups []Group) error {
	// Читаем конфиг sing-box.
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cannot read config: %w", err)
	}

	// Удаляем комментарии перед парсингом.
	cleanedConfigData := removeComments(configData)

	var config map[string]interface{}

	if err := json.Unmarshal(cleanedConfigData, &config); err != nil {
		return fmt.Errorf("cannot parse config: %w", err)
	}

	// Получаем секцию route.
	route, ok := config["route"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("route section not found in config")
	}

	// Получаем rule_set.
	var ruleSets []interface{}

	if rs, ok := route["rule_set"].([]interface{}); ok {
		ruleSets = rs
	} else {
		ruleSets = []interface{}{}
	}

	// Получаем rules.
	var rules []interface{}

	if r, ok := route["rules"].([]interface{}); ok {
		rules = r
	} else {
		rules = []interface{}{}
	}

	// Удаляем старые ruleset'ы для групп (кроме default/configurer для обратной совместимости).
	newRuleSets := []interface{}{}

	for _, rs := range ruleSets {
		rsMap, ok := rs.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := rsMap["tag"].(string)
		if !ok {
			continue
		}

		// Сохраняем только ruleset с тегом "configurer" для обратной совместимости.
		if tag == "configurer" {
			newRuleSets = append(newRuleSets, rs)
		}
	}

	// Добавляем ruleset для каждой группы.
	for _, group := range groups {
		ruleSet := map[string]interface{}{
			"format": "source",
			"http_client": map[string]interface{}{
				"tag": "default_http_client",
			},
			"tag":             "configurer-" + group.Name,
			"type":            "remote",
			"update_interval": "30s",
			"url":             fmt.Sprintf("http://127.0.0.1:8080/api/ruleset/group?group=%s", group.Name),
		}

		newRuleSets = append(newRuleSets, ruleSet)
	}

	route["rule_set"] = newRuleSets

	// Удаляем старые правила для групп из rules.
	newRules := []interface{}{}
	foundSniff := false
	foundHijackDNS := false
	foundResolve := false
	foundPrivate := false

	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]interface{})
		if !ok {
			newRules = append(newRules, rule)

			continue
		}

		// Проверяем служебные правила.
		if action, ok := ruleMap["action"].(string); ok {
			if action == "sniff" {
				foundSniff = true
			}

			if action == "hijack-dns" {
				foundHijackDNS = true
			}

			if action == "resolve" {
				foundResolve = true
			}
		}

		if _, ok := ruleMap["ip_is_private"]; ok {
			foundPrivate = true
		}

		// Пропускаем старое правило с ruleset "configurer".
		if ruleSetTag, ok := ruleMap["rule_set"].(string); ok {
			if ruleSetTag == "configurer" {
				continue
			}
		}

		// Сохраняем все остальные правила.
		newRules = append(newRules, rule)
	}

	// Добавляем служебные правила, если их нет.
	serviceRules := []interface{}{}

	if !foundSniff {
		serviceRules = append(serviceRules, map[string]interface{}{
			"action": "sniff",
		})
	}

	if !foundHijackDNS {
		serviceRules = append(serviceRules, map[string]interface{}{
			"action":   "hijack-dns",
			"protocol": "dns",
		})
	}

	if !foundResolve {
		serviceRules = append(serviceRules, map[string]interface{}{
			"action":   "resolve",
			"strategy": "ipv4_only",
		})
	}

	if !foundPrivate {
		serviceRules = append(serviceRules, map[string]interface{}{
			"ip_is_private": true,
			"outbound":      "direct",
		})
	}

	// Добавляем правила для каждой группы в конец.
	for _, group := range groups {
		groupRule := map[string]interface{}{
			"outbound": "select-" + group.Name,
			"rule_set": "configurer-" + group.Name,
		}

		serviceRules = append(serviceRules, groupRule)
	}

	// Объединяем пользовательские и служебные правила.
	finalRules := append(newRules, serviceRules...)
	route["rules"] = finalRules

	// Проверяем наличие селекторов для групп в outbounds.
	if err := ensureSelectorsInConfig(config, groups); err != nil {
		return fmt.Errorf("cannot ensure selectors: %w", err)
	}

	// Сохраняем конфиг обратно.
	configData, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	return nil
}

// ensureSelectorsInConfig проверяет и добавляет селекторы для групп в outbounds.
func ensureSelectorsInConfig(config map[string]interface{}, groups []Group) error {
	outbounds, ok := config["outbounds"].([]interface{})
	if !ok {
		return fmt.Errorf("outbounds section not found in config")
	}

	// Собираем существующие селекторы.
	existingSelectors := make(map[string]bool)

	for _, ob := range outbounds {
		obMap, ok := ob.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := obMap["tag"].(string)
		if !ok {
			continue
		}

		obType, ok := obMap["type"].(string)
		if !ok {
			continue
		}

		if obType == "selector" {
			existingSelectors[tag] = true
		}
	}

	// Получаем список всех outbounds для использования в selector.
	var availableOutbounds []string

	for _, ob := range outbounds {
		obMap, ok := ob.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := obMap["tag"].(string)
		if !ok {
			continue
		}

		obType, ok := obMap["type"].(string)
		if !ok {
			continue
		}

		// Добавляем только VPN outbounds и direct.
		if obType != "selector" && obType != "block" && tag != "dns-out" {
			availableOutbounds = append(availableOutbounds, tag)
		}
	}

	// Добавляем селекторы для групп, если их нет.
	newOutbounds := make([]interface{}, len(outbounds))
	copy(newOutbounds, outbounds)

	for _, group := range groups {
		selectorTag := "select-" + group.Name

		if !existingSelectors[selectorTag] {
			defaultOutbound := group.DefaultOutbound
			if defaultOutbound == "" {
				defaultOutbound = "direct"
			}

			selector := map[string]interface{}{
				"default":                     defaultOutbound,
				"interrupt_exist_connections": true,
				"outbounds":                   availableOutbounds,
				"tag":                         selectorTag,
				"type":                        "selector",
			}

			newOutbounds = append(newOutbounds, selector)
		}
	}

	config["outbounds"] = newOutbounds

	return nil
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
