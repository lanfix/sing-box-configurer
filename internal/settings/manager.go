// Package settings хранит в app.json общие настройки: уровень логов sing-box, доступ к Clash API,
// плановую перезагрузку, применение подписок Happ и защиту панели.
package settings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/cron"
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

// Restart — плановая перезагрузка sing-box по расписанию.
type Restart struct {
	Enabled bool `json:"enabled"`

	// Schedule — расписание в формате crontab: минута час день месяц день_недели.
	Schedule string `json:"schedule"`

	// Timezone — часовой пояс расписания (например, UTC или Europe/Moscow).
	Timezone string `json:"timezone"`
}

// SourcesProxy — служебный mixed-inbound sing-box, через который конфигуратор загружает URL-источники
// с detour: логин — тег outbound-а, пароль общий.
type SourcesProxy struct {
	Password string `json:"password"`
}

// Happ — применение обновлений подписок Happ.
type Happ struct {
	// AutoApply — обновленные серверы подписок сразу переносятся в рабочий конфиг sing-box. Остальные
	// неприменённые изменения при этом не применяются.
	AutoApply bool `json:"auto_apply"`
}

// Settings — общие настройки.
type Settings struct {
	LogLevel string   `json:"log_level"`
	ClashAPI ClashAPI `json:"clash_api"`
	Restart  Restart  `json:"restart"`

	// SourcesProxy генерируется при первом запуске.
	SourcesProxy SourcesProxy `json:"sources_proxy"`

	Happ      Happ      `json:"happ"`
	Security  Security  `json:"security"`
	SpeedTest SpeedTest `json:"speed_test"`
}

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	Settings *Settings `json:"settings"`
}

// presenceSection показывает, какие разделы настроек уже сохранены: у разделов, которых нет в app.json
// (данные прежних версий), включаемые по умолчанию флаги иначе прочитались бы как false.
type presenceSection struct {
	Settings *struct {
		Happ      *Happ      `json:"happ"`
		Security  *Security  `json:"security"`
		SpeedTest *SpeedTest `json:"speed_test"`
	} `json:"settings"`
}

// DefaultHapp возвращает настройки подписок Happ по умолчанию: обновления применяются сразу.
func DefaultHapp() Happ {
	return Happ{
		AutoApply: true,
	}
}

// DefaultRestart возвращает расписание перезагрузки по умолчанию: ежедневно в 06:00 UTC,
// как делал отдельный контейнер cron-scheduler.
func DefaultRestart() Restart {
	return Restart{
		Enabled:  true,
		Schedule: "0 6 * * *",
		Timezone: "UTC",
	}
}

// Default возвращает настройки новой инсталляции (без секрета — он генерируется при загрузке).
func Default() Settings {
	return Settings{
		LogLevel: "warn",
		ClashAPI: ClashAPI{
			Secret:       "",
			AllowOrigins: []string{"*"},
		},
		Restart: DefaultRestart(),
		SourcesProxy: SourcesProxy{
			Password: "",
		},
		Happ:      DefaultHapp(),
		Security:  DefaultSecurity(),
		SpeedTest: DefaultSpeedTest(),
	}
}

// Validate проверяет расписание и часовой пояс перезагрузки.
func (r *Restart) Validate() error {
	if _, err := cron.Parse(r.Schedule); err != nil {
		return fmt.Errorf("расписание перезагрузки: %w", err)
	}

	if _, err := time.LoadLocation(r.Timezone); err != nil {
		return fmt.Errorf("неизвестный часовой пояс %q", r.Timezone)
	}

	return nil
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

	var presence presenceSection

	if err := appData.Read(&presence); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read settings: %w", err)
	}

	if presence.Settings == nil || presence.Settings.Happ == nil {
		m.data.Happ = DefaultHapp()
		changed = true
	}

	if presence.Settings == nil || presence.Settings.Security == nil {
		m.data.Security = DefaultSecurity()
		changed = true
	}

	if presence.Settings == nil || presence.Settings.SpeedTest == nil || len(m.data.SpeedTest.Servers) == 0 {
		m.data.SpeedTest = DefaultSpeedTest()
		changed = true
	}

	if m.data.Security.AllowedHosts == nil {
		m.data.Security.AllowedHosts = []string{}
		changed = true
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

	if m.data.SourcesProxy.Password == "" {
		password, err := GenerateSecret()
		if err != nil {
			return nil, err
		}

		m.data.SourcesProxy.Password = password
		changed = true
	}

	// Настроек перезагрузки еще нет: переносим поведение контейнера cron-scheduler (06:00 UTC).
	if m.data.Restart.Schedule == "" {
		m.data.Restart = DefaultRestart()
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
	result.Security.AllowedHosts = slices.Clone(m.data.Security.AllowedHosts)
	result.SpeedTest.Servers = slices.Clone(m.data.SpeedTest.Servers)

	return result
}

// UpdateSpeedTest меняет настройки теста скорости outbound-ов. Действуют сразу.
func (m *Manager) UpdateSpeedTest(speedTest SpeedTest) error {
	normalized, err := speedTest.Normalize()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.SpeedTest = normalized

	return m.save()
}

// UpdateHapp меняет настройки применения подписок Happ. Действуют сразу.
func (m *Manager) UpdateHapp(happ Happ) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.Happ = happ

	return m.save()
}

// UpdateSecurity меняет защиту панели. Действует сразу, без применения конфига.
func (m *Manager) UpdateSecurity(security Security) error {
	normalized, err := security.Normalize()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.Security = normalized

	return m.save()
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

// UpdateRestart меняет расписание плановой перезагрузки sing-box. Применяется сразу, без применения конфига.
func (m *Manager) UpdateRestart(restart Restart) error {
	restart.Schedule = strings.Join(strings.Fields(restart.Schedule), " ")
	restart.Timezone = strings.TrimSpace(restart.Timezone)

	if restart.Timezone == "" {
		restart.Timezone = "UTC"
	}

	if err := restart.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.Restart = restart

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
