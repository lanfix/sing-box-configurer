package happ

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
)

const (
	// Интервал обновления подписки, если сервер его не прислал.
	defaultUpdateInterval = 3 * time.Hour

	// Как часто фоновый процесс проверяет, пора ли обновлять профили.
	backgroundCheckInterval = 5 * time.Minute

	// Таймаут на загрузку одной подписки.
	fetchTimeout = 30 * time.Second
)

// Manager управляет профилями Happ: загрузкой и обновлением подписок. Серверы профилей попадают
// в конфиг sing-box при рендере.
type Manager struct {
	store  *Store
	client *Client

	// Сериализует операции над профилями, чтобы фоновое обновление не пересекалось с действиями из UI.
	mu sync.Mutex
}

// NewManager создает менеджер профилей Happ.
func NewManager(store *Store, client *Client) *Manager {
	return &Manager{
		store:  store,
		client: client,
		mu:     sync.Mutex{},
	}
}

// InstallationID возвращает ID инсталляции (HWID), под которым сервис представляется серверам подписок.
func (m *Manager) InstallationID() string {
	return m.store.InstallationID()
}

// List возвращает профили.
func (m *Manager) List() []Profile {
	return m.store.List()
}

// Subscriptions возвращает outbound-ы серверов каждого профиля для рендера конфига.
func (m *Manager) Subscriptions() []outbound.Subscription {
	profiles := m.store.List()
	subscriptions := make([]outbound.Subscription, 0, len(profiles))

	for _, profile := range profiles {
		outbounds := make([]map[string]any, 0, len(profile.Servers))

		for _, server := range profile.Servers {
			outbounds = append(outbounds, jsonmap.Clone(server.Outbound))
		}

		subscriptions = append(subscriptions, outbound.Subscription{
			Source:      outbound.SourceHapp,
			ProfileID:   profile.ID,
			ProfileName: profile.Name,
			Outbounds:   outbounds,
		})
	}

	return subscriptions
}

// Add загружает подписку и сохраняет профиль.
func (m *Manager) Add(ctx context.Context, name, subscriptionURL string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	parsedURL, err := url.Parse(strings.TrimSpace(subscriptionURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid subscription url")
	}

	for _, existing := range m.store.List() {
		if existing.URL == parsedURL.String() {
			return nil, fmt.Errorf("profile with this url already exists: %s", existing.Name)
		}
	}

	subscription, err := m.fetch(ctx, parsedURL.String())
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = coalesce(strings.TrimSpace(subscription.Info.Title), parsedURL.Host)
	}

	for _, existing := range m.store.List() {
		if existing.Name == name {
			return nil, fmt.Errorf("profile with name %q already exists", name)
		}
	}

	parsed, err := parseSubscription(subscription, name)
	if err != nil {
		return nil, err
	}

	profile := Profile{
		ID:         uuid.NewString(),
		Name:       name,
		URL:        parsedURL.String(),
		Info:       &subscription.Info,
		Servers:    parsed.Servers,
		Warnings:   parsed.Warnings,
		LastUpdate: time.Now(),
		LastError:  "",
		CreatedAt:  time.Now(),
	}

	if err = m.store.Put(profile); err != nil {
		return nil, fmt.Errorf("cannot save profile: %w", err)
	}

	return &profile, nil
}

// Refresh заново загружает подписку профиля.
func (m *Manager) Refresh(ctx context.Context, id string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.refreshLocked(ctx, id)
}

// Delete удаляет профиль.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.store.Get(id); !ok {
		return fmt.Errorf("profile not found")
	}

	return m.store.Delete(id)
}

// Start запускает фоновое обновление информации по профилям с интервалом, который задает сервер подписки.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(backgroundCheckInterval)
		defer ticker.Stop()

		for {
			m.refreshDue(ctx)

			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
			}
		}
	}()
}

// refreshDue обновляет профили, у которых истек интервал обновления.
func (m *Manager) refreshDue(ctx context.Context) {
	for _, profile := range m.store.List() {
		interval := defaultUpdateInterval

		if profile.Info != nil && profile.Info.UpdateInterval > 0 {
			interval = time.Duration(profile.Info.UpdateInterval) * time.Hour
		}

		if time.Since(profile.LastUpdate) < interval {
			continue
		}

		if _, err := m.Refresh(ctx, profile.ID); err != nil {
			log.Printf("happ: cannot refresh profile %q: %v", profile.Name, err)
		}
	}
}

// refreshLocked загружает подписку и обновляет профиль. Ошибка сохраняется в профиле.
func (m *Manager) refreshLocked(ctx context.Context, id string) (*Profile, error) {
	profile, ok := m.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("profile not found")
	}

	profile.LastUpdate = time.Now()

	subscription, err := m.fetch(ctx, profile.URL)
	if err == nil {
		var parsed *ParseResult

		if parsed, err = parseSubscription(subscription, profile.Name); err == nil {
			profile.Info = &subscription.Info
			profile.Servers = parsed.Servers
			profile.Warnings = parsed.Warnings
		}
	}

	profile.LastError = ""

	if err != nil {
		profile.LastError = err.Error()
	}

	if saveErr := m.store.Put(profile); saveErr != nil {
		return nil, fmt.Errorf("cannot save profile: %w", saveErr)
	}

	if err != nil {
		return nil, err
	}

	return &profile, nil
}

// fetch загружает подписку с таймаутом.
func (m *Manager) fetch(ctx context.Context, subscriptionURL string) (*Subscription, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	subscription, err := m.client.Fetch(ctx, subscriptionURL, m.store.InstallationID())
	if err != nil {
		return nil, fmt.Errorf("cannot fetch subscription: %w", err)
	}

	return subscription, nil
}

// parseSubscription разбирает подписку. Если серверов нет, в ошибку добавляется объявление сервера,
// т.к. панели сообщают через него о проблемах (например, о превышении лимита устройств).
func parseSubscription(subscription *Subscription, profileName string) (*ParseResult, error) {
	parsed, err := ParseSubscription(subscription.Body, profileName)
	if err == nil {
		return parsed, nil
	}

	if details := coalesce(subscription.Info.Announce, subscription.Info.Title); details != "" {
		return nil, fmt.Errorf("%w (%s)", err, details)
	}

	return nil, err
}
