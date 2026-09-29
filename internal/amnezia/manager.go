package amnezia

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

const (
	// Как часто обновлять конфигурацию Amnezia Premium в фоне (сведения о подписке, срок ключа).
	premiumRefreshInterval = 12 * time.Hour

	// Как часто фоновый процесс проверяет, пора ли обновлять.
	backgroundCheckInterval = 30 * time.Minute

	// Как долго кэшировать результат проверки поддержки AmneziaWG: рендер конфига вызывает ее часто.
	supportCacheTTL = time.Minute
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

// Manager импортирует конфигурации Amnezia. Их серверы попадают в конфиг sing-box при рендере.
type Manager struct {
	appData  *appdata.File
	versions VersionSource
	gateway  *GatewayClient

	// Сериализует операции над профилями, включая запросы к шлюзу и фоновое обновление.
	mu       sync.Mutex
	profiles []Profile

	// Кэш проверки поддержки AmneziaWG.
	supportMu        sync.Mutex
	support          Support
	supportCheckedAt time.Time
}

// NewManager загружает профили из app.json.
func NewManager(appData *appdata.File, versions VersionSource, gateway *GatewayClient) (*Manager, error) {
	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read amnezia data: %w", err)
	}

	profiles := section.Amnezia.Profiles
	if profiles == nil {
		profiles = []Profile{}
	}

	return &Manager{
		appData:          appData,
		versions:         versions,
		gateway:          gateway,
		mu:               sync.Mutex{},
		profiles:         profiles,
		supportMu:        sync.Mutex{},
		support:          Support{},
		supportCheckedAt: time.Time{},
	}, nil
}

// AWGSupport проверяет, поддерживает ли запущенный sing-box AmneziaWG.
// Поддержка есть в форке sing-box-lx (версии вида "1.14.1-lx.8"); официальный sing-box ее не имеет.
func (m *Manager) AWGSupport() Support {
	m.supportMu.Lock()
	defer m.supportMu.Unlock()

	if !m.supportCheckedAt.IsZero() && time.Since(m.supportCheckedAt) < supportCacheTTL {
		return m.support
	}

	version, err := m.versions.GetVersion()

	m.supportCheckedAt = time.Now()
	m.support = Support{
		Supported: err == nil && strings.Contains(version, "-lx"),
		Version:   version,
		Error:     "",
	}

	if err != nil {
		m.support.Error = "не удалось узнать версию sing-box: " + err.Error()
	}

	return m.support
}

// List возвращает профили.
func (m *Manager) List() []Profile {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]Profile, len(m.profiles))
	copy(result, m.profiles)

	return result
}

// Subscriptions возвращает outbound-ы и endpoint-ы каждого профиля для рендера конфига. Серверы AmneziaWG
// пропускаются, если запущенный sing-box точно их не поддерживает: иначе он не запустится с неизвестными
// полями. Для пропущенных серверов возвращаются предупреждения.
func (m *Manager) Subscriptions() ([]outbound.Subscription, []string) {
	profiles := m.List()
	support := m.AWGSupport()

	// Версию узнать не удалось — поддержку не отрицаем, конфиг все равно проверит sing-box check.
	skipAWG := support.Error == "" && !support.Supported

	subscriptions := make([]outbound.Subscription, 0, len(profiles))
	warnings := make([]string, 0)

	for _, profile := range profiles {
		outbounds := make([]map[string]any, 0, len(profile.Items))

		for _, item := range profile.Items {
			if item.RequiresAWG && skipAWG {
				warnings = append(warnings, fmt.Sprintf("Amnezia: сервер %s пропущен — %s не поддерживает AmneziaWG", item.Tag, support.Version))

				continue
			}

			outbounds = append(outbounds, jsonmap.Clone(item.Config))
		}

		subscriptions = append(subscriptions, outbound.Subscription{
			Source:      outbound.SourceAmnezia,
			ProfileID:   profile.ID,
			ProfileName: profile.Name,
			Outbounds:   outbounds,
		})
	}

	return subscriptions, warnings
}

// Add импортирует ключ vpn:// — конфигурацию своего сервера или подписку Amnezia Premium (формат
// определяется автоматически).
func (m *Manager) Add(ctx context.Context, key, name string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	premium, data, err := decodePremiumKey(key)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = coalesce(strings.TrimSpace(data.Name), strings.TrimSpace(data.Description), data.HostName, "Amnezia")
	}

	for _, existing := range m.profiles {
		if existing.Name == name {
			return nil, fmt.Errorf("профиль с именем %q уже есть", name)
		}
	}

	var parsed *Parsed

	if premium != nil {
		parsed, err = m.gateway.FetchConfig(ctx, premium, name)
	} else {
		parsed, err = Parse(key, name)
	}

	if err != nil {
		return nil, err
	}

	profile := Profile{
		ID:          uuid.NewString(),
		Name:        name,
		Description: parsed.Description,
		Server:      parsed.Server,
		Items:       parsed.Items,
		Warnings:    parsed.Warnings,
		Premium:     premium,
		LastUpdate:  time.Now(),
		LastError:   "",
		CreatedAt:   time.Now(),
	}

	m.profiles = append(m.profiles, profile)

	if err = m.saveLocked(); err != nil {
		return nil, err
	}

	return &profile, nil
}

// Refresh заново запрашивает конфигурацию подписки у шлюза.
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

// Delete удаляет профиль. Для подписки Amnezia Premium освобождается место устройства; если шлюз
// недоступен, профиль все равно удаляется, а ошибка возвращается как предупреждение.
func (m *Manager) Delete(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(id)
	if index < 0 {
		return "", fmt.Errorf("профиль не найден")
	}

	profile := m.profiles[index]
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
