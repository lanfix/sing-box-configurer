package amnezia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

const (
	// Как часто обновлять конфигурацию Amnezia Premium в фоне (сведения о подписке, срок ключа).
	premiumRefreshInterval = 12 * time.Hour

	// Как часто фоновый процесс проверяет, пора ли обновлять.
	backgroundCheckInterval = 30 * time.Minute
)

// Profile — импортированная конфигурация Amnezia.
type Profile struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Server      string        `json:"server"`
	Items       []Item        `json:"items"`
	Warnings    []string      `json:"warnings,omitempty"`
	Premium     *PremiumState `json:"premium,omitempty"`
	SyncedTags  []string      `json:"synced_tags"`
	SyncedHash  string        `json:"synced_hash"`
	LastUpdate  time.Time     `json:"last_update"`
	LastError   string        `json:"last_error,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
}

// RequiresAWG возвращает true, если для профиля нужен sing-box с поддержкой AmneziaWG.
func (p *Profile) RequiresAWG() bool {
	for _, item := range p.Items {
		if item.RequiresAWG {
			return true
		}
	}

	return false
}

// ItemsHash возвращает хеш текущих серверов профиля.
func (p *Profile) ItemsHash() string {
	raw, _ := json.Marshal(p.Items)
	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:])
}

// OutOfSync возвращает true, если серверы профиля в конфиге устарели.
func (p *Profile) OutOfSync() bool {
	return len(p.SyncedTags) > 0 && p.SyncedHash != p.ItemsHash()
}

// OutboundWriter заменяет outbounds/endpoints во временном конфиге sing-box.
type OutboundWriter interface {
	ReplaceOutbounds(removeTags []string, add []map[string]any) error
}

// VersionSource возвращает версию запущенного sing-box (например, через Clash API).
type VersionSource interface {
	GetVersion() (string, error)
}

// Support — поддерживает ли запущенный sing-box AmneziaWG.
type Support struct {
	Supported bool   `json:"supported"`
	Version   string `json:"version"`
	Error     string `json:"error,omitempty"`
}

// appDataSection — раздел app.json с профилями Amnezia.
type appDataSection struct {
	Amnezia struct {
		Profiles []Profile `json:"profiles"`
	} `json:"amnezia"`
}

// Manager импортирует конфигурации Amnezia и записывает их во временный конфиг sing-box.
type Manager struct {
	appData   *appdata.File
	outbounds OutboundWriter
	versions  VersionSource
	gateway   *GatewayClient

	// Сериализует операции над профилями, включая запросы к шлюзу и фоновое обновление.
	mu       sync.Mutex
	profiles []Profile
}

// NewManager загружает профили из app.json.
func NewManager(appData *appdata.File, outbounds OutboundWriter, versions VersionSource, gateway *GatewayClient) (*Manager, error) {
	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read amnezia data: %w", err)
	}

	profiles := section.Amnezia.Profiles
	if profiles == nil {
		profiles = []Profile{}
	}

	return &Manager{
		appData:   appData,
		outbounds: outbounds,
		versions:  versions,
		gateway:   gateway,
		mu:        sync.Mutex{},
		profiles:  profiles,
	}, nil
}

// AWGSupport проверяет, поддерживает ли запущенный sing-box AmneziaWG.
// Поддержка есть в форке sing-box-lx (версии вида "1.14.1-lx.8"); официальный sing-box ее не имеет.
func (m *Manager) AWGSupport() Support {
	version, err := m.versions.GetVersion()
	if err != nil {
		return Support{
			Supported: false,
			Version:   "",
			Error:     "не удалось узнать версию sing-box: " + err.Error(),
		}
	}

	return Support{
		Supported: strings.Contains(version, "-lx"),
		Version:   version,
		Error:     "",
	}
}

// List возвращает профили.
func (m *Manager) List() []Profile {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]Profile, len(m.profiles))
	copy(result, m.profiles)

	return result
}

// Add импортирует ключ vpn:// — конфигурацию своего сервера или подписку Amnezia Premium (формат
// определяется автоматически). Если sing-box не поддерживает AmneziaWG, профиль сохраняется, но в конфиг
// не записывается: иначе sing-box не запустится с неизвестными полями.
func (m *Manager) Add(ctx context.Context, key, name string) (*Profile, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	premium, data, err := decodePremiumKey(key)
	if err != nil {
		return nil, false, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = coalesce(strings.TrimSpace(data.Name), strings.TrimSpace(data.Description), data.HostName, "Amnezia")
	}

	for _, existing := range m.profiles {
		if existing.Name == name {
			return nil, false, fmt.Errorf("профиль с именем %q уже есть", name)
		}
	}

	var parsed *Parsed

	if premium != nil {
		parsed, err = m.gateway.FetchConfig(ctx, premium, name)
	} else {
		parsed, err = Parse(key, name)
	}

	if err != nil {
		return nil, false, err
	}

	profile := Profile{
		ID:          uuid.NewString(),
		Name:        name,
		Description: parsed.Description,
		Server:      parsed.Server,
		Items:       parsed.Items,
		Warnings:    parsed.Warnings,
		Premium:     premium,
		SyncedTags:  []string{},
		SyncedHash:  "",
		LastUpdate:  time.Now(),
		LastError:   "",
		CreatedAt:   time.Now(),
	}

	synced := false

	if !profile.RequiresAWG() || m.AWGSupport().Supported {
		if err = m.writeLocked(&profile); err != nil {
			return nil, false, err
		}

		synced = true
	}

	m.profiles = append(m.profiles, profile)

	if err = m.saveLocked(); err != nil {
		return nil, false, err
	}

	return &profile, synced, nil
}

// Refresh заново запрашивает конфигурацию подписки у шлюза. Конфиг sing-box не изменяется.
func (m *Manager) Refresh(ctx context.Context, id string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.refreshLocked(ctx, id)
}

// SetCountry меняет страну сервера подписки и запрашивает новую конфигурацию.
func (m *Manager) SetCountry(ctx context.Context, id, country string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(id)
	if index < 0 {
		return nil, fmt.Errorf("профиль не найден")
	}

	premium := m.profiles[index].Premium
	if premium == nil {
		return nil, fmt.Errorf("страну можно выбрать только для Amnezia Premium")
	}

	if !premium.hasCountry(country) {
		return nil, fmt.Errorf("страна %q недоступна в подписке", country)
	}

	previous := premium.ServerCountryCode
	premium.ServerCountryCode = country

	profile, err := m.refreshLocked(ctx, id)
	if err != nil {
		premium.ServerCountryCode = previous
		_ = m.saveLocked()

		return nil, err
	}

	return profile, nil
}

// Sync записывает серверы профиля во временный конфиг sing-box.
func (m *Manager) Sync(id string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(id)
	if index < 0 {
		return nil, fmt.Errorf("профиль не найден")
	}

	profile := &m.profiles[index]

	if profile.RequiresAWG() {
		if support := m.AWGSupport(); !support.Supported {
			return nil, fmt.Errorf("sing-box %s не поддерживает AmneziaWG: нужен форк sing-box-lx", coalesce(support.Version, "неизвестной версии"))
		}
	}

	if err := m.writeLocked(profile); err != nil {
		return nil, err
	}

	result := *profile

	return &result, m.saveLocked()
}

// Delete удаляет профиль и его серверы из временного конфига sing-box. Для подписки Amnezia Premium
// освобождается место устройства; если шлюз недоступен, профиль все равно удаляется, а ошибка
// возвращается как предупреждение.
func (m *Manager) Delete(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(id)
	if index < 0 {
		return "", fmt.Errorf("профиль не найден")
	}

	profile := m.profiles[index]

	if len(profile.SyncedTags) > 0 {
		if err := m.outbounds.ReplaceOutbounds(profile.SyncedTags, nil); err != nil {
			return "", fmt.Errorf("cannot remove outbounds: %w", err)
		}
	}

	warning := ""

	if profile.Premium != nil {
		if err := m.gateway.RevokeConfig(ctx, profile.Premium); err != nil {
			warning = "не удалось освободить место устройства в подписке: " + err.Error()
		}
	}

	m.profiles = append(m.profiles[:index], m.profiles[index+1:]...)

	return warning, m.saveLocked()
}

// Start запускает фоновое обновление подписок Amnezia Premium.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(backgroundCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				m.refreshDue(ctx)
			}
		}
	}()
}

// refreshDue обновляет подписки, которые давно не обновлялись.
func (m *Manager) refreshDue(ctx context.Context) {
	for _, profile := range m.List() {
		if profile.Premium == nil || time.Since(profile.LastUpdate) < premiumRefreshInterval {
			continue
		}

		if _, err := m.Refresh(ctx, profile.ID); err != nil {
			log.Printf("amnezia: cannot refresh profile %q: %v", profile.Name, err)
		}
	}
}

// refreshLocked запрашивает конфигурацию подписки у шлюза. Ошибка сохраняется в профиле.
func (m *Manager) refreshLocked(ctx context.Context, id string) (*Profile, error) {
	index := m.indexLocked(id)
	if index < 0 {
		return nil, fmt.Errorf("профиль не найден")
	}

	profile := &m.profiles[index]

	if profile.Premium == nil {
		return nil, fmt.Errorf("обновлять можно только подписку Amnezia Premium: конфигурация своего сервера не меняется")
	}

	parsed, err := m.gateway.FetchConfig(ctx, profile.Premium, profile.Name)

	profile.LastUpdate = time.Now()
	profile.LastError = ""

	if err != nil {
		profile.LastError = err.Error()
	} else {
		profile.Description = parsed.Description
		profile.Server = parsed.Server
		profile.Items = parsed.Items
		profile.Warnings = parsed.Warnings
	}

	if saveErr := m.saveLocked(); saveErr != nil {
		return nil, saveErr
	}

	if err != nil {
		return nil, err
	}

	result := *profile

	return &result, nil
}

// writeLocked заменяет ранее записанные серверы профиля на актуальные.
func (m *Manager) writeLocked(profile *Profile) error {
	add := make([]map[string]any, 0, len(profile.Items))
	tags := make([]string, 0, len(profile.Items))

	for _, item := range profile.Items {
		add = append(add, item.Config)
		tags = append(tags, item.Tag)
	}

	if err := m.outbounds.ReplaceOutbounds(profile.SyncedTags, add); err != nil {
		return fmt.Errorf("cannot write outbounds: %w", err)
	}

	profile.SyncedTags = tags
	profile.SyncedHash = profile.ItemsHash()

	return nil
}

// indexLocked возвращает индекс профиля или -1.
func (m *Manager) indexLocked(id string) int {
	for i := range m.profiles {
		if m.profiles[i].ID == id {
			return i
		}
	}

	return -1
}

// saveLocked сохраняет профили в app.json.
func (m *Manager) saveLocked() error {
	var section appDataSection

	section.Amnezia.Profiles = m.profiles

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save amnezia data: %w", err)
	}

	return nil
}
