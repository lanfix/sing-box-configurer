package outbound

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// Теги встроенных outbound-ов, которые создает рендер конфига.
const (
	AutoTag   = "auto"
	DirectTag = "direct"
	BlockTag  = "block"

	// SelectorTagPrefix — префикс тегов selector-ов групп (select-<группа>).
	SelectorTagPrefix = "select-"
)

// ErrNotFound — outbound-а с таким ID нет.
var ErrNotFound = errors.New("outbound not found")

// Item — outbound (или endpoint), добавленный вручную. Config — объект sing-box как есть.
type Item struct {
	ID        string         `json:"id"`
	Config    map[string]any `json:"config"`
	CreatedAt time.Time      `json:"created_at"`
}

// Tag возвращает тег outbound-а.
func (i *Item) Tag() string {
	return jsonmap.String(i.Config, "tag")
}

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	Outbounds *[]Item `json:"outbounds"`
}

// IsReservedTag проверяет, что тег занят встроенным outbound-ом или selector-ом группы.
func IsReservedTag(tag string) bool {
	return tag == AutoTag || tag == DirectTag || tag == BlockTag || strings.HasPrefix(tag, SelectorTagPrefix)
}

// IsEndpoint проверяет, что объект — endpoint sing-box (WireGuard), а не outbound.
func IsEndpoint(config map[string]any) bool {
	return jsonmap.String(config, "type") == "wireguard"
}

// Manager хранит outbound-ы, добавленные вручную, в разделе "outbounds" файла app.json.
type Manager struct {
	appData *appdata.File
	mu      sync.RWMutex
	items   []Item
}

// NewManager загружает outbound-ы из app.json.
func NewManager(appData *appdata.File) (*Manager, error) {
	m := &Manager{
		appData: appData,
		mu:      sync.RWMutex{},
		items:   []Item{},
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read outbounds: %w", err)
	}

	if section.Outbounds != nil {
		m.items = *section.Outbounds
	}

	return m, nil
}

// List возвращает копию списка outbound-ов.
func (m *Manager) List() []Item {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Item, len(m.items))

	for i, item := range m.items {
		result[i] = item
		result[i].Config = jsonmap.Clone(item.Config)
	}

	return result
}

// AddFromShare добавляет outbound из share-ссылки. Если тег уже занят, к нему добавляется суффикс.
func (m *Manager) AddFromShare(shareURL string) (*Item, error) {
	share, err := ParseShareUrl(shareURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse share url: %w", err)
	}

	config, err := jsonmap.Normalize(share.GetOutbound().Config)
	if err != nil {
		return nil, fmt.Errorf("cannot convert outbound: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	configMap := config.(map[string]any)
	configMap["tag"] = m.uniqueTagLocked(jsonmap.String(configMap, "tag"))

	return m.addLocked(configMap)
}

// Add добавляет outbound, заданный объектом sing-box.
func (m *Manager) Add(config map[string]any) (*Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.addLocked(config)
}

// Update заменяет объект outbound-а с указанным ID.
func (m *Manager) Update(id string, config map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(id)
	if index < 0 {
		return ErrNotFound
	}

	if err := m.validateLocked(config, id); err != nil {
		return err
	}

	m.items[index].Config = jsonmap.Clone(config)

	return m.save()
}

// Delete удаляет outbound с указанным ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(id)
	if index < 0 {
		return ErrNotFound
	}

	m.items = slices.Delete(m.items, index, index+1)

	return m.save()
}

// addLocked проверяет и добавляет outbound (без блокировки).
func (m *Manager) addLocked(config map[string]any) (*Item, error) {
	if err := m.validateLocked(config, ""); err != nil {
		return nil, err
	}

	item := Item{
		ID:        uuid.NewString(),
		Config:    jsonmap.Clone(config),
		CreatedAt: time.Now(),
	}

	m.items = append(m.items, item)

	if err := m.save(); err != nil {
		return nil, err
	}

	return &item, nil
}

// validateLocked проверяет объект outbound-а. exceptID — ID редактируемого outbound-а (без блокировки).
func (m *Manager) validateLocked(config map[string]any, exceptID string) error {
	tag := strings.TrimSpace(jsonmap.String(config, "tag"))

	if tag == "" {
		return fmt.Errorf("у outbound-а должен быть тег (поле tag)")
	}

	if jsonmap.String(config, "type") == "" {
		return fmt.Errorf("у outbound-а должен быть тип (поле type)")
	}

	if IsReservedTag(tag) {
		return fmt.Errorf("тег %s зарезервирован (auto, direct, block и select-*)", tag)
	}

	for _, item := range m.items {
		if item.ID != exceptID && item.Tag() == tag {
			return fmt.Errorf("outbound с тегом %s уже существует", tag)
		}
	}

	return nil
}

// uniqueTagLocked возвращает свободный тег на основе tag (без блокировки).
func (m *Manager) uniqueTagLocked(tag string) string {
	taken := func(candidate string) bool {
		return IsReservedTag(candidate) || slices.ContainsFunc(m.items, func(item Item) bool {
			return item.Tag() == candidate
		})
	}

	if !taken(tag) {
		return tag
	}

	for i := 2; ; i++ {
		if candidate := fmt.Sprintf("%s-%d", tag, i); !taken(candidate) {
			return candidate
		}
	}
}

// indexLocked возвращает индекс outbound-а с ID id или -1 (без блокировки).
func (m *Manager) indexLocked(id string) int {
	return slices.IndexFunc(m.items, func(item Item) bool {
		return item.ID == id
	})
}

// save сохраняет outbound-ы в app.json (без блокировки).
func (m *Manager) save() error {
	section := appDataSection{
		Outbounds: &m.items,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save outbounds: %w", err)
	}

	return nil
}
