package singbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type ConfigManager struct {
	configPath    string
	tempPath      string
	mu            sync.RWMutex
	hasPending    bool
	pendingConfig string
}

func NewConfigManager(configPath string) *ConfigManager {
	tempDir := os.TempDir()
	tempPath := filepath.Join(tempDir, "sing-box-config-temp.json")

	// Проверяем, существует ли временный файл.
	hasPending := false
	if _, err := os.Stat(tempPath); err == nil {
		hasPending = true
	}

	return &ConfigManager{
		configPath: configPath,
		tempPath:   tempPath,
		hasPending: hasPending,
	}
}

// GetConfig читает и возвращает текущий конфиг sing-box.
func (m *ConfigManager) GetConfig() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return "", fmt.Errorf("cannot read config: %w", err)
	}

	// Проверяем JSON (с комментариями).
	if err := validateJSONWithComments(data); err != nil {
		return "", fmt.Errorf("cannot parse json: %w", err)
	}

	return string(data), nil
}

// SaveTemp сохраняет конфиг во временный файл без применения.
func (m *ConfigManager) SaveTemp(configContent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Валидируем JSON (с комментариями).
	if err := validateJSONWithComments([]byte(configContent)); err != nil {
		return fmt.Errorf("cannot parse json: %w", err)
	}

	if err := os.WriteFile(m.tempPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("cannot write temp config: %w", err)
	}

	m.hasPending = true
	m.pendingConfig = configContent

	return nil
}

// GetTemp возвращает временный конфиг, если он существует.
func (m *ConfigManager) GetTemp() (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.hasPending {
		return "", false, nil
	}

	data, err := os.ReadFile(m.tempPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.hasPending = false

			return "", false, nil
		}

		return "", false, fmt.Errorf("cannot read temp config: %w", err)
	}

	return string(data), true, nil
}

// ApplyConfig применяет временный конфиг к основному файлу конфигурации.
func (m *ConfigManager) ApplyConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.hasPending {
		return fmt.Errorf("no pending config to apply")
	}

	data, err := os.ReadFile(m.tempPath)
	if err != nil {
		return fmt.Errorf("cannot read temp config: %w", err)
	}

	// Валидируем JSON (с комментариями).
	if err := validateJSONWithComments(data); err != nil {
		return fmt.Errorf("cannot parse json in temp config: %w", err)
	}

	backupPath := m.configPath + ".backup"
	currentData, err := os.ReadFile(m.configPath)
	if err == nil {
		_ = os.WriteFile(backupPath, currentData, 0644)
	}

	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	m.hasPending = false
	m.pendingConfig = ""
	_ = os.Remove(m.tempPath)

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

// validateJSONWithComments валидирует JSON с поддержкой комментариев.
func validateJSONWithComments(data []byte) error {
	cleaned := removeComments(data)

	var jsonCheck interface{}

	if err := json.Unmarshal(cleaned, &jsonCheck); err != nil {
		return err
	}

	return nil
}

// DiscardTemp удаляет временный конфиг.
func (m *ConfigManager) DiscardTemp() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.hasPending {
		return nil
	}

	m.hasPending = false
	m.pendingConfig = ""
	_ = os.Remove(m.tempPath)

	return nil
}

// HasPending возвращает true, если есть несохраненный конфиг.
func (m *ConfigManager) HasPending() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.hasPending
}
