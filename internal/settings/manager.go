// Package settings хранит в app.json общие настройки sing-box: уровень логов и доступ к Clash API.
package settings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// logLevels — уровни логов sing-box.
var logLevels = []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}

// ClashAPI — доступ к Clash API sing-box.
type ClashAPI struct {
	// Secret — токен доступа (заголовок Authorization: Bearer).
	Secret string `json:"secret"`

	// AllowOrigins — origin-ы, которым разрешены CORS-запросы (например, внешние панели вроде yacd).
	AllowOrigins []string `json:"allow_origins"`
}

// Settings — общие настройки sing-box.
type Settings struct {
	LogLevel string   `json:"log_level"`
	ClashAPI ClashAPI `json:"clash_api"`
}

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	Settings *Settings `json:"settings"`
}

// Default возвращает настройки новой инсталляции (без секрета — он генерируется при загрузке).
func Default() Settings {
	return Settings{
		LogLevel: "warn",
		ClashAPI: ClashAPI{
			Secret:       "",
			AllowOrigins: []string{"*"},
		},
	}
}

// Manager хранит настройки в разделе "settings" файла app.json.
type Manager struct {
	appData *appdata.File
	mu      sync.RWMutex
	data    Settings
}

// NewManager загружает настройки. Недостающие значения заполняются по умолчанию, секрет Clash API
// генерируется при первом запуске.
func NewManager(appData *appdata.File) (*Manager, error) {
	m := &Manager{
		appData: appData,
		mu:      sync.RWMutex{},
		data:    Default(),
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read settings: %w", err)
	}

	changed := section.Settings == nil

	if section.Settings != nil {
		m.data = *section.Settings
	}

	if m.data.LogLevel == "" {
		m.data.LogLevel = "warn"
		changed = true
	}

	if m.data.ClashAPI.AllowOrigins == nil {
		m.data.ClashAPI.AllowOrigins = []string{"*"}
		changed = true
	}

	if m.data.ClashAPI.Secret == "" {
		secret, err := GenerateSecret()
		if err != nil {
			return nil, err
		}

		m.data.ClashAPI.Secret = secret
		changed = true
	}

	if changed {
		if err := m.save(); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// Get возвращает копию настроек.
func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := m.data
	result.ClashAPI.AllowOrigins = slices.Clone(m.data.ClashAPI.AllowOrigins)

	return result
}

// Update меняет уровень логов и список CORS-origin-ов Clash API.
func (m *Manager) Update(logLevel string, allowOrigins []string) error {
	logLevel = strings.TrimSpace(logLevel)

	if !slices.Contains(logLevels, logLevel) {
		return fmt.Errorf("некорректный уровень логов %q", logLevel)
	}

	origins := make([]string, 0, len(allowOrigins))

	for _, origin := range allowOrigins {
		origin = strings.TrimSpace(origin)

		if origin != "" && !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.LogLevel = logLevel
	m.data.ClashAPI.AllowOrigins = origins

	return m.save()
}

// RegenerateSecret создает новый секрет Clash API. Он вступит в силу после применения конфига.
func (m *Manager) RegenerateSecret() error {
	secret, err := GenerateSecret()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.ClashAPI.Secret = secret

	return m.save()
}

// save сохраняет настройки в app.json (без блокировки).
func (m *Manager) save() error {
	section := appDataSection{
		Settings: &m.data,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save settings: %w", err)
	}

	return nil
}

// GenerateSecret возвращает случайный секрет длиной 32 байта в hex.
func GenerateSecret() (string, error) {
	buf := make([]byte, 32)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot generate secret: %w", err)
	}

	return hex.EncodeToString(buf), nil
}
