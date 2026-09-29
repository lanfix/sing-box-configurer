package dnsconfig

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// ErrNotFound — сервера с таким тегом нет.
var ErrNotFound = errors.New("dns server not found")

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	DNS *Data `json:"dns"`
}

// Manager хранит DNS-настройки в разделе "dns" файла app.json.
type Manager struct {
	appData *appdata.File
	mu      sync.RWMutex
	data    Data
}

// NewManager загружает DNS-настройки из app.json. Если раздела нет, сохраняются настройки по умолчанию.
func NewManager(appData *appdata.File) (*Manager, error) {
	m := &Manager{
		appData: appData,
		mu:      sync.RWMutex{},
		data:    Default(),
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read dns settings: %w", err)
	}

	if section.DNS == nil {
		if err := m.save(); err != nil {
			return nil, err
		}

		return m, nil
	}

	m.data = *section.DNS

	if m.data.Servers == nil {
		m.data.Servers = []Server{}
	}

	if m.data.Rules == nil {
		m.data.Rules = []map[string]any{}
	}

	return m, nil
}

// Get возвращает копию DNS-настроек.
func (m *Manager) Get() Data {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return Data{
		Servers:  jsonmapCloneServers(m.data.Servers),
		Settings: m.data.Settings,
		Rules:    jsonmap.Clone(m.data.Rules),
	}
}

// ServerTags возвращает теги серверов в порядке их объявления.
func (m *Manager) ServerTags() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return serverTags(m.data.Servers)
}

// AddServer добавляет сервер.
func (m *Manager) AddServer(server Server) error {
	server.Normalize()

	m.mu.Lock()
	defer m.mu.Unlock()

	if slices.Contains(serverTags(m.data.Servers), server.Tag) {
		return fmt.Errorf("сервер с тегом %s уже существует", server.Tag)
	}

	if err := m.validateServerLocked(server); err != nil {
		return err
	}

	m.data.Servers = append(m.data.Servers, server)

	return m.save()
}

// EditServer заменяет параметры сервера с тегом server.Tag. Тег сервера не меняется.
func (m *Manager) EditServer(server Server) error {
	server.Normalize()

	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(server.Tag)
	if index < 0 {
		return ErrNotFound
	}

	if err := m.validateServerLocked(server); err != nil {
		return err
	}

	m.data.Servers[index] = server

	return m.save()
}

// DeleteServer удаляет сервер. Сервер, на который ссылаются настройки или другие серверы, удалить нельзя.
func (m *Manager) DeleteServer(tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(tag)
	if index < 0 {
		return ErrNotFound
	}

	if m.data.Settings.Final == tag {
		return fmt.Errorf("сервер %s используется как final", tag)
	}

	if m.data.Settings.DefaultDomainResolver == tag {
		return fmt.Errorf("сервер %s используется как default_domain_resolver", tag)
	}

	for _, server := range m.data.Servers {
		if server.DomainResolver == tag {
			return fmt.Errorf("сервер %s используется для резолва адреса сервера %s", tag, server.Tag)
		}
	}

	m.data.Servers = slices.Delete(m.data.Servers, index, index+1)

	return m.save()
}

// UpdateSettings заменяет общие параметры DNS из формы. Дополнительные поля секции dns (Extra)
// не меняются: они сохраняются вместе с правилами через UpdateAdvanced.
func (m *Manager) UpdateSettings(settings Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	settings.Extra = jsonmap.Clone(m.data.Settings.Extra)
	settings.Normalize()

	if err := settings.Validate(serverTags(m.data.Servers)); err != nil {
		return err
	}

	m.data.Settings = settings

	return m.save()
}

// UpdateAdvanced заменяет пользовательские DNS-правила и дополнительные поля секции dns.
func (m *Manager) UpdateAdvanced(rules []map[string]any, extra map[string]any) error {
	for i, rule := range rules {
		if len(rule) == 0 {
			return fmt.Errorf("правило %d пустое", i+1)
		}
	}

	if err := ValidateExtra(extra); err != nil {
		return err
	}

	if len(extra) == 0 {
		extra = nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.Rules = jsonmap.Clone(rules)
	m.data.Settings.Extra = jsonmap.Clone(extra)

	return m.save()
}

// validateServerLocked проверяет сервер и ссылки на другие серверы (без блокировки).
func (m *Manager) validateServerLocked(server Server) error {
	if err := server.Validate(); err != nil {
		return err
	}

	if server.DomainResolver != "" && !slices.Contains(serverTags(m.data.Servers), server.DomainResolver) {
		return fmt.Errorf("сервер %q для domain_resolver не найден", server.DomainResolver)
	}

	return nil
}

// indexLocked возвращает индекс сервера с тегом tag или -1 (без блокировки).
func (m *Manager) indexLocked(tag string) int {
	return slices.IndexFunc(m.data.Servers, func(server Server) bool {
		return server.Tag == tag
	})
}

// save сохраняет настройки в app.json (без блокировки).
func (m *Manager) save() error {
	section := appDataSection{
		DNS: &m.data,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save dns settings: %w", err)
	}

	return nil
}

// serverTags возвращает теги серверов.
func serverTags(servers []Server) []string {
	tags := make([]string, 0, len(servers))

	for _, server := range servers {
		tags = append(tags, server.Tag)
	}

	return tags
}

// jsonmapCloneServers копирует список серверов вместе с их Extra.
func jsonmapCloneServers(servers []Server) []Server {
	result := make([]Server, len(servers))

	for i, server := range servers {
		result[i] = server

		if server.Extra != nil {
			result[i].Extra = jsonmap.Clone(server.Extra)
		}
	}

	return result
}
