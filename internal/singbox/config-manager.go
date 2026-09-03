package singbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	return &ConfigManager{
		configPath: configPath,
		tempPath:   tempPath,
		hasPending: false,
	}
}

// GetConfig reads and returns the current sing-box config.
func (m *ConfigManager) GetConfig() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	var jsonCheck interface{}

	if err := json.Unmarshal(data, &jsonCheck); err != nil {
		return "", fmt.Errorf("cannot unmarshal json: %w", err)
	}

	return string(data), nil
}

// SaveTemp saves the config to a temporary file without applying it.
func (m *ConfigManager) SaveTemp(configContent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var jsonCheck interface{}

	if err := json.Unmarshal([]byte(configContent), &jsonCheck); err != nil {
		return fmt.Errorf("cannot unmarshal json: %w", err)
	}

	// Write to temp file.
	if err := os.WriteFile(m.tempPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("cannot write temp config: %w", err)
	}

	m.hasPending = true
	m.pendingConfig = configContent

	return nil
}

// GetTemp returns the temporary config if exists.
func (m *ConfigManager) GetTemp() (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.hasPending {
		return "", false, nil
	}

	data, err := os.ReadFile(m.tempPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read temp config: %w", err)
	}

	return string(data), true, nil
}

// ApplyConfig applies the temporary config to the actual config file.
func (m *ConfigManager) ApplyConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.hasPending {
		return fmt.Errorf("no pending config to apply")
	}

	// Read temp config.
	data, err := os.ReadFile(m.tempPath)
	if err != nil {
		return fmt.Errorf("failed to read temp config: %w", err)
	}

	var jsonCheck interface{}

	if err := json.Unmarshal(data, &jsonCheck); err != nil {
		return fmt.Errorf("invalid JSON in temp config: %w", err)
	}

	// Backup current config.
	backupPath := m.configPath + ".backup"
	currentData, err := os.ReadFile(m.configPath)

	if err == nil {
		_ = os.WriteFile(backupPath, currentData, 0644)
	}

	// Write new config.
	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Clear pending state.
	m.hasPending = false
	m.pendingConfig = ""

	_ = os.Remove(m.tempPath)

	return nil
}

// DiscardTemp removes the temporary config.
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

// HasPending returns true if there's a pending config.
func (m *ConfigManager) HasPending() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.hasPending
}
