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

func NewProvider(actualConfigPath string) *Provider {
	tempDir := os.TempDir()
	tempConfigPath := filepath.Join(tempDir, "sing-box-config-temp.json")

	return &Provider{
		actualConfigPath: actualConfigPath,
		tempConfigPath:   tempConfigPath,
	}
}

// GetConfig читает и возвращает текущий конфиг sing-box (как есть).
func (p *Provider) GetConfig() ([]byte, error) {
	configData, err := os.ReadFile(p.actualConfigPath)
	if err != nil {
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

// GetTemp возвращает временный конфиг, если он существует.
func (p *Provider) GetTemp() ([]byte, error) {
	configData, err := os.ReadFile(p.tempConfigPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read temp config: %w", err)
	}

	return configData, nil
}

// SaveTemp сохраняет конфиг во временный файл без применения.
func (p *Provider) SaveTemp(configData []byte) error {
	// Валидируем JSON (с комментариями).
	if err := validateJSONWithComments(configData); err != nil {
		return fmt.Errorf("cannot parse json: %w", err)
	}

	if err := os.WriteFile(p.tempConfigPath, configData, 0644); err != nil {
		return fmt.Errorf("cannot write temp config: %w", err)
	}

	return nil
}

// ApplyConfig применяет временный конфиг к основному файлу конфигурации.
func (p *Provider) ApplyConfig() error {
	content, err := p.GetTemp()
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

	_ = p.RemoveTemp()

	return nil
}

// RemoveTemp удаляет временный конфиг.
func (p *Provider) RemoveTemp() error {
	if err := os.Remove(p.tempConfigPath); err != nil {
		return fmt.Errorf("cannot remove temp config: %w", err)
	}

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
