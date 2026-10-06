// Package inbounds хранит в app.json inbound-ы sing-box, которые добавляются через интерфейс (mixed-прокси).
// Встроенные inbound-ы (tun-in, dns-in) задаются рендером и здесь не хранятся.
package inbounds

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// Теги встроенных inbound-ов.
const (
	TunTag = "tun-in"
	DNSTag = "dns-in"
)

var (
	// ErrNotFound — inbound-а с таким тегом нет.
	ErrNotFound = errors.New("inbound not found")

	// tagRe — допустимый тег inbound-а.
	tagRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// User — пользователь mixed-прокси.
type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Mixed — mixed-прокси (HTTP и SOCKS на одном порту).
type Mixed struct {
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
	Users      []User `json:"users"`

	// Outbound — куда уходит весь трафик прокси первым правилом маршрутизации. Пусто — по общим правилам.
	Outbound string `json:"outbound,omitempty"`

	Extra map[string]any `json:"extra,omitempty"`
}

// Data — раздел "inbounds" файла app.json.
type Data struct {
	Mixed []Mixed `json:"mixed"`
}

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	Inbounds *Data `json:"inbounds"`
}

// Config возвращает объект inbound-а для конфига sing-box.
func (m *Mixed) Config() map[string]any {
	result := map[string]any{
		"type":        "mixed",
		"tag":         m.Tag,
		"listen":      m.Listen,
		"listen_port": m.ListenPort,
	}

	if len(m.Users) > 0 {
		users := make([]any, 0, len(m.Users))

		for _, user := range m.Users {
			users = append(users, map[string]any{
				"username": user.Username,
				"password": user.Password,
			})
		}

		result["users"] = users
	}

	jsonmap.Merge(result, m.Extra)

	return result
}

// MixedFromConfig разбирает mixed-inbound из конфига sing-box. Неизвестные поля попадают в Extra.
func MixedFromConfig(config map[string]any) Mixed {
	mixed := Mixed{
		Tag:        jsonmap.String(config, "tag"),
		Listen:     jsonmap.String(config, "listen"),
		ListenPort: jsonmap.Int(config, "listen_port"),
		Users:      []User{},
		Outbound:   "",
		Extra:      jsonmap.Without(config, "type", "tag", "listen", "listen_port", "users"),
	}

	users, _ := config["users"].([]any)

	for _, item := range users {
		user, ok := item.(map[string]any)
		if !ok {
			continue
		}

		mixed.Users = append(mixed.Users, User{
			Username: jsonmap.String(user, "username"),
			Password: jsonmap.String(user, "password"),
		})
	}

	if len(mixed.Extra) == 0 {
		mixed.Extra = nil
	}

	return mixed
}

// normalize обрезает пробелы и подставляет адрес по умолчанию.
func (m *Mixed) normalize() {
	m.Tag = strings.TrimSpace(m.Tag)
	m.Listen = strings.TrimSpace(m.Listen)
	m.Outbound = strings.TrimSpace(m.Outbound)

	if m.Listen == "" {
		m.Listen = "0.0.0.0"
	}

	if m.Users == nil {
		m.Users = []User{}
	}

	if len(m.Extra) == 0 {
		m.Extra = nil
	}
}

// validate проверяет поля inbound-а.
func (m *Mixed) validate() error {
	if !tagRe.MatchString(m.Tag) {
		return fmt.Errorf("некорректный тег %q", m.Tag)
	}

	if m.Tag == TunTag || m.Tag == DNSTag {
		return fmt.Errorf("тег %s занят встроенным inbound-ом", m.Tag)
	}

	if _, err := netip.ParseAddr(m.Listen); err != nil {
		return fmt.Errorf("некорректный адрес %q", m.Listen)
	}

	if m.ListenPort < 1 || m.ListenPort > 65535 {
		return fmt.Errorf("некорректный порт %d", m.ListenPort)
	}

	for _, user := range m.Users {
		if user.Username == "" || user.Password == "" {
			return fmt.Errorf("у пользователя должны быть имя и пароль")
		}
	}

	return nil
}

// Manager хранит inbound-ы в разделе "inbounds" файла app.json.
type Manager struct {
	appData *appdata.File
	mu      sync.RWMutex
	data    Data
}

// NewManager загружает inbound-ы из app.json.
func NewManager(appData *appdata.File) (*Manager, error) {
	m := &Manager{
		appData: appData,
		mu:      sync.RWMutex{},
		data: Data{
			Mixed: []Mixed{},
		},
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read inbounds: %w", err)
	}

	if section.Inbounds != nil && section.Inbounds.Mixed != nil {
		m.data = *section.Inbounds
	}

	return m, nil
}

// Mixed возвращает копию списка mixed-inbound-ов.
func (m *Manager) Mixed() []Mixed {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Mixed, len(m.data.Mixed))

	for i, mixed := range m.data.Mixed {
		result[i] = mixed
		result[i].Users = slices.Clone(mixed.Users)

		if mixed.Extra != nil {
			result[i].Extra = jsonmap.Clone(mixed.Extra)
		}
	}

	return result
}

// AddMixed добавляет mixed-inbound.
func (m *Manager) AddMixed(mixed Mixed) error {
	mixed.normalize()

	if err := mixed.validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.indexLocked(mixed.Tag) >= 0 {
		return fmt.Errorf("inbound с тегом %s уже существует", mixed.Tag)
	}

	m.data.Mixed = append(m.data.Mixed, mixed)

	return m.save()
}

// EditMixed заменяет параметры mixed-inbound-а с тегом mixed.Tag.
func (m *Manager) EditMixed(mixed Mixed) error {
	mixed.normalize()

	if err := mixed.validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(mixed.Tag)
	if index < 0 {
		return ErrNotFound
	}

	m.data.Mixed[index] = mixed

	return m.save()
}

// DeleteMixed удаляет mixed-inbound.
func (m *Manager) DeleteMixed(tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(tag)
	if index < 0 {
		return ErrNotFound
	}

	m.data.Mixed = slices.Delete(m.data.Mixed, index, index+1)

	return m.save()
}

// UsingOutbound возвращает теги mixed-inbound-ов, весь трафик которых уходит в outbound с тегом tag.
func (m *Manager) UsingOutbound(tag string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0)

	for _, mixed := range m.data.Mixed {
		if tag != "" && mixed.Outbound == tag {
			result = append(result, mixed.Tag)
		}
	}

	return result
}

// indexLocked возвращает индекс inbound-а с тегом tag или -1 (без блокировки).
func (m *Manager) indexLocked(tag string) int {
	return slices.IndexFunc(m.data.Mixed, func(mixed Mixed) bool {
		return mixed.Tag == tag
	})
}

// save сохраняет inbound-ы в app.json (без блокировки).
func (m *Manager) save() error {
	section := appDataSection{
		Inbounds: &m.data,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save inbounds: %w", err)
	}

	return nil
}
