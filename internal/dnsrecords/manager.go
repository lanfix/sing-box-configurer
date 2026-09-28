// Package dnsrecords хранит DNS-записи (домен → адреса), которые отдает DNS sing-box.
package dnsrecords

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

var (
	// ErrNotFound — записи с таким ID нет.
	ErrNotFound = errors.New("dns record not found")

	// domainRe — допустимое имя домена (метки из латиницы, цифр, дефиса и подчеркивания).
	domainRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*$`)
)

// Record описывает DNS-запись.
type Record struct {
	ID          string    `json:"id"`
	Domain      string    `json:"domain"`
	Addresses   []string  `json:"addresses"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	DNSRecords []Record `json:"dns_records"`
}

// Manager хранит DNS-записи в разделе "dns_records" файла app.json.
type Manager struct {
	appData *appdata.File
	mu      sync.RWMutex
	records []Record
}

// NewManager загружает DNS-записи из app.json.
func NewManager(appData *appdata.File) (*Manager, error) {
	m := &Manager{
		appData: appData,
		mu:      sync.RWMutex{},
		records: []Record{},
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read dns records: %w", err)
	}

	if section.DNSRecords != nil {
		m.records = section.DNSRecords
	}

	return m, nil
}

// List возвращает копию списка записей.
func (m *Manager) List() []Record {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Clone(m.records)
}

// Add добавляет запись.
func (m *Manager) Add(domain string, addresses []string, description string) (Record, error) {
	domain, addresses, err := normalize(domain, addresses)
	if err != nil {
		return Record{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.hasDomain(domain, "") {
		return Record{}, fmt.Errorf("запись для домена %s уже существует", domain)
	}

	record := Record{
		ID:          uuid.NewString(),
		Domain:      domain,
		Addresses:   addresses,
		Description: description,
		CreatedAt:   time.Now(),
	}

	m.records = append(m.records, record)

	if err = m.save(); err != nil {
		return Record{}, err
	}

	return record, nil
}

// Edit обновляет запись с указанным ID.
func (m *Manager) Edit(id, domain string, addresses []string, description string) error {
	domain, addresses, err := normalize(domain, addresses)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.hasDomain(domain, id) {
		return fmt.Errorf("запись для домена %s уже существует", domain)
	}

	index := slices.IndexFunc(m.records, func(record Record) bool {
		return record.ID == id
	})

	if index < 0 {
		return ErrNotFound
	}

	m.records[index].Domain = domain
	m.records[index].Addresses = addresses
	m.records[index].Description = description

	return m.save()
}

// Delete удаляет запись с указанным ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := slices.IndexFunc(m.records, func(record Record) bool {
		return record.ID == id
	})

	if index < 0 {
		return ErrNotFound
	}

	m.records = slices.Delete(m.records, index, index+1)

	return m.save()
}

// hasDomain проверяет, есть ли запись с доменом domain, кроме записи с ID exceptID (без блокировки).
func (m *Manager) hasDomain(domain, exceptID string) bool {
	return slices.ContainsFunc(m.records, func(record Record) bool {
		return record.Domain == domain && record.ID != exceptID
	})
}

// save сохраняет записи в app.json, не затрагивая данные других менеджеров (без блокировки).
func (m *Manager) save() error {
	section := appDataSection{
		DNSRecords: m.records,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save dns records: %w", err)
	}

	return nil
}

// normalize приводит домен к нижнему регистру без завершающей точки и проверяет адреса.
func normalize(domain string, addresses []string) (string, []string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")

	if !domainRe.MatchString(domain) {
		return "", nil, fmt.Errorf("некорректный домен: %q", domain)
	}

	result := make([]string, 0, len(addresses))

	for _, address := range addresses {
		address = strings.TrimSpace(address)

		if address == "" {
			continue
		}

		parsed, err := netip.ParseAddr(address)
		if err != nil {
			return "", nil, fmt.Errorf("некорректный IP-адрес: %q", address)
		}

		if value := parsed.String(); !slices.Contains(result, value) {
			result = append(result, value)
		}
	}

	if len(result) == 0 {
		return "", nil, errors.New("нужен хотя бы один IP-адрес")
	}

	return domain, result, nil
}
