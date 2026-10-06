package devices

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
	"github.com/lanfix/sing-box-configurer/internal/rules"
)

const (
	// scanInterval — как часто читается таблица соседей хоста.
	scanInterval = time.Minute

	// seenSaveInterval — как часто сохраняется время последнего появления устройств без других изменений.
	seenSaveInterval = 30 * time.Minute

	// seenTTL и maxSeen ограничивают список найденных устройств.
	seenTTL = 30 * 24 * time.Hour
	maxSeen = 300
)

// ErrUnknownProfile — rule-set для такого профиля не отдается.
var ErrUnknownProfile = errors.New("unknown device profile")

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	Devices *Data `json:"devices"`
}

// Options — зависимости менеджера. Без Network устройства не находятся, без SingBox поддержка не проверяется
// и профили не рендерятся.
type Options struct {
	Network  platform.Network
	SingBox  platform.SingBox
	Versions VersionSource
}

// DeviceView — устройство для интерфейса: профиль и последние сведения из таблицы соседей.
type DeviceView struct {
	MAC       string     `json:"mac"`
	Name      string     `json:"name"`
	Profile   string     `json:"profile"`
	AddedAt   *time.Time `json:"added_at,omitempty"`
	IPs       []string   `json:"ips"`
	Interface string     `json:"interface"`
	FirstSeen *time.Time `json:"first_seen,omitempty"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	Online    bool       `json:"online"`
}

// State — данные страницы «Устройства».
type State struct {
	// Devices — устройства с профилем, Unknown — найденные в сети, но не добавленные.
	Devices []DeviceView `json:"devices"`
	Unknown []DeviceView `json:"unknown"`

	Settings Settings `json:"settings"`
	Detected Detected `json:"detected"`

	// Networks и Exclusions — итоговые сети LAN и исключения профиля по умолчанию.
	Networks   []string `json:"networks"`
	Exclusions []string `json:"exclusions"`

	ScannedAt *time.Time `json:"scanned_at,omitempty"`
	ScanError string     `json:"scan_error,omitempty"`
	Support   Support    `json:"support"`
}

// Manager хранит профили устройств в разделе "devices" файла app.json.
type Manager struct {
	appData *appdata.File
	network platform.Network
	support *supportChecker

	mu   sync.RWMutex
	data Data

	// online — MAC-адреса из последнего чтения таблицы соседей.
	online    map[string]bool
	scannedAt time.Time
	scanError string
	savedAt   time.Time
}

// NewManager загружает профили устройств из app.json.
func NewManager(appData *appdata.File, opts Options) (*Manager, error) {
	m := &Manager{
		appData: appData,
		network: opts.Network,
		support: &supportChecker{
			checker:   opts.SingBox,
			versions:  opts.Versions,
			mu:        sync.Mutex{},
			support:   Support{},
			attemptAt: time.Time{},
		},
		mu: sync.RWMutex{},
		data: Data{
			Devices:  []Device{},
			Settings: DefaultSettings(),
			Detected: Detected{
				Networks:      []string{},
				HostAddresses: []string{},
				UpdatedAt:     time.Time{},
			},
			Seen: []Seen{},
		},
		online:    map[string]bool{},
		scannedAt: time.Time{},
		scanError: "",
		savedAt:   time.Time{},
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read devices: %w", err)
	}

	if section.Devices != nil {
		m.load(*section.Devices)
	}

	return m, nil
}

// load переносит данные из app.json, подставляя значения по умолчанию для пустых полей.
func (m *Manager) load(data Data) {
	if data.Devices != nil {
		m.data.Devices = data.Devices
	}

	if data.Settings.DefaultProfile != "" {
		m.data.Settings = data.Settings
	}

	if m.data.Settings.Networks == nil {
		m.data.Settings.Networks = []string{}
	}

	if m.data.Settings.Exclude == nil {
		m.data.Settings.Exclude = []string{}
	}

	if data.Detected.Networks != nil {
		m.data.Detected.Networks = data.Detected.Networks
	}

	if data.Detected.HostAddresses != nil {
		m.data.Detected.HostAddresses = data.Detected.HostAddresses
	}

	m.data.Detected.UpdatedAt = data.Detected.UpdatedAt

	if data.Seen != nil {
		m.data.Seen = data.Seen
	}
}

// Start читает таблицу соседей и проверяет поддержку sing-box сразу и затем раз в минуту.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()

		lastError := ""

		for {
			scanCtx, cancel := context.WithTimeout(ctx, scanInterval/2)

			// Одна и та же ошибка пишется в журнал один раз.
			if err := m.Refresh(scanCtx); err != nil && err.Error() != lastError {
				log.Printf("Devices: cannot read host network: %v", err)
				lastError = err.Error()
			} else if err == nil {
				lastError = ""
			}

			cancel()

			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
			}
		}
	}()
}

// Refresh читает таблицу соседей хоста и проверяет поддержку профилей устройств запущенным sing-box.
func (m *Manager) Refresh(ctx context.Context) error {
	err := m.Scan(ctx)

	m.support.refresh(ctx)

	return err
}

// Scan читает таблицу соседей хоста: обновляет найденные устройства, сети LAN и адреса хоста.
func (m *Manager) Scan(ctx context.Context) error {
	if m.network == nil {
		return errors.New("платформа не умеет читать сеть хоста")
	}

	output, err := m.network.Read(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.scannedAt = now

	if err != nil {
		m.scanError = err.Error()

		return err
	}

	m.scanError = ""

	network := HostNetwork{
		Addresses: ParseAddresses(output.Addresses),
		Neighbors: ParseNeighbors(output.Neighbors),
	}

	changed := m.updateDetectedLocked(network, now)
	changed = m.updateSeenLocked(network.lanNeighbors(), now) || changed

	if !changed && time.Since(m.savedAt) < seenSaveInterval {
		return nil
	}

	return m.saveLocked()
}

// updateDetectedLocked запоминает найденные сети LAN и адреса хоста. Пустой результат не затирает прежний:
// политика не должна пропадать, пока таблица соседей пуста.
func (m *Manager) updateDetectedLocked(network HostNetwork, now time.Time) bool {
	networks, hostAddresses := network.Detect()
	changed := false

	if len(networks) > 0 && !slices.Equal(networks, m.data.Detected.Networks) {
		m.data.Detected.Networks = networks
		changed = true
	}

	if len(hostAddresses) > 0 && !slices.Equal(hostAddresses, m.data.Detected.HostAddresses) {
		m.data.Detected.HostAddresses = hostAddresses
		changed = true
	}

	if len(networks) > 0 {
		m.data.Detected.UpdatedAt = now
	}

	return changed
}

// updateSeenLocked обновляет найденные устройства. Возвращает true, если появилось новое устройство
// или у известного сменились адреса.
func (m *Manager) updateSeenLocked(neighbors []Neighbor, now time.Time) bool {
	current := map[string]*Seen{}

	for _, neighbor := range neighbors {
		entry, ok := current[neighbor.MAC]
		if !ok {
			entry = &Seen{
				MAC:       neighbor.MAC,
				IPs:       []string{},
				Interface: neighbor.Interface,
				FirstSeen: now,
				LastSeen:  now,
			}
			current[neighbor.MAC] = entry
		}

		entry.IPs = appendUnique(entry.IPs, neighbor.IP.String())
	}

	changed := false
	online := make(map[string]bool, len(current))

	for mac, entry := range current {
		online[mac] = true
		slices.Sort(entry.IPs)

		index := slices.IndexFunc(m.data.Seen, func(seen Seen) bool {
			return seen.MAC == mac
		})

		if index < 0 {
			m.data.Seen = append(m.data.Seen, *entry)
			changed = true

			continue
		}

		seen := &m.data.Seen[index]

		if !slices.Equal(seen.IPs, entry.IPs) || seen.Interface != entry.Interface {
			seen.IPs = entry.IPs
			seen.Interface = entry.Interface
			changed = true
		}

		seen.LastSeen = now
	}

	m.online = online

	return m.trimSeenLocked(now) || changed
}

// trimSeenLocked удаляет устройства, которых давно не было в сети, и ограничивает размер списка.
func (m *Manager) trimSeenLocked(now time.Time) bool {
	before := len(m.data.Seen)

	m.data.Seen = slices.DeleteFunc(m.data.Seen, func(seen Seen) bool {
		return now.Sub(seen.LastSeen) > seenTTL
	})

	if len(m.data.Seen) > maxSeen {
		sort.Slice(m.data.Seen, func(i, j int) bool {
			return m.data.Seen[i].LastSeen.After(m.data.Seen[j].LastSeen)
		})

		m.data.Seen = m.data.Seen[:maxSeen]
	}

	return len(m.data.Seen) != before
}

// State возвращает данные для страницы «Устройства».
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]DeviceView, 0, len(m.data.Devices))
	added := map[string]bool{}

	for _, device := range m.data.Devices {
		addedAt := device.AddedAt
		view := m.viewLocked(device.MAC)
		view.Name = device.Name
		view.Profile = device.Profile
		view.AddedAt = &addedAt

		devices = append(devices, view)
		added[device.MAC] = true
	}

	unknown := make([]DeviceView, 0)

	for _, seen := range m.data.Seen {
		if !added[seen.MAC] {
			view := m.viewLocked(seen.MAC)
			view.Profile = ProfileDefault

			unknown = append(unknown, view)
		}
	}

	sort.SliceStable(unknown, func(i, j int) bool {
		return unknown[i].LastSeen.After(*unknown[j].LastSeen)
	})

	state := State{
		Devices:    devices,
		Unknown:    unknown,
		Settings:   m.data.Settings,
		Detected:   m.data.Detected,
		Networks:   m.data.networks(),
		Exclusions: m.data.exclusions(),
		ScannedAt:  nil,
		ScanError:  m.scanError,
		Support:    m.support.get(),
	}

	if !m.scannedAt.IsZero() {
		scannedAt := m.scannedAt
		state.ScannedAt = &scannedAt
	}

	return state
}

// viewLocked возвращает устройство mac со сведениями из таблицы соседей.
func (m *Manager) viewLocked(mac string) DeviceView {
	view := DeviceView{
		MAC:       mac,
		Name:      "",
		Profile:   "",
		AddedAt:   nil,
		IPs:       []string{},
		Interface: "",
		FirstSeen: nil,
		LastSeen:  nil,
		Online:    m.online[mac],
	}

	for _, seen := range m.data.Seen {
		if seen.MAC != mac {
			continue
		}

		firstSeen, lastSeen := seen.FirstSeen, seen.LastSeen

		view.IPs = slices.Clone(seen.IPs)
		view.Interface = seen.Interface
		view.FirstSeen = &firstSeen
		view.LastSeen = &lastSeen
	}

	return view
}

// UnknownCount возвращает число найденных устройств без профиля (точка в меню).
func (m *Manager) UnknownCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0

	for _, seen := range m.data.Seen {
		if m.indexLocked(seen.MAC) < 0 {
			count++
		}
	}

	return count
}

// Add добавляет устройство.
func (m *Manager) Add(device Device) error {
	if err := device.normalize(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.indexLocked(device.MAC) >= 0 {
		return fmt.Errorf("устройство %s уже добавлено", device.MAC)
	}

	device.AddedAt = time.Now().UTC()
	m.data.Devices = append(m.data.Devices, device)

	return m.saveLocked()
}

// Edit меняет имя и профиль устройства с MAC-адресом device.MAC.
func (m *Manager) Edit(device Device) error {
	if err := device.normalize(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(device.MAC)
	if index < 0 {
		return ErrNotFound
	}

	device.AddedAt = m.data.Devices[index].AddedAt
	m.data.Devices[index] = device

	return m.saveLocked()
}

// Delete удаляет устройство: оно снова становится неизвестным.
func (m *Manager) Delete(mac string) error {
	mac = NormalizeMAC(mac)

	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.indexLocked(mac)
	if index < 0 {
		return ErrNotFound
	}

	m.data.Devices = slices.Delete(m.data.Devices, index, index+1)

	return m.saveLocked()
}

// Settings возвращает политику для неизвестных устройств.
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	settings := m.data.Settings
	settings.Networks = slices.Clone(settings.Networks)
	settings.Exclude = slices.Clone(settings.Exclude)

	return settings
}

// UpdateSettings сохраняет политику для неизвестных устройств.
func (m *Manager) UpdateSettings(settings Settings) error {
	if err := settings.normalize(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data.Settings = settings

	return m.saveLocked()
}

// Enabled сообщает, рендерить ли профили устройств: только если проверка показала, что sing-box их поддерживает.
// Без результата проверки sing-box проверяется сразу.
func (m *Manager) Enabled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), supportTimeout)
	defer cancel()

	return m.support.ensure(ctx, supportRetry).Enabled()
}

// RefreshSupport перепроверяет поддержку, если сменилась версия sing-box (например, после обновления).
func (m *Manager) RefreshSupport(ctx context.Context) {
	m.support.refresh(ctx)
}

// RuleSet возвращает rule-set профиля для sing-box. Если sing-box не поддерживает MAC-адреса в rule-set-ах
// или поддержка не проверена, набор пустой: иначе sing-box не принял бы его и не запустился.
func (m *Manager) RuleSet(ctx context.Context, profile string) (rules.SingBoxRuleSet, error) {
	if !slices.Contains(RuleSetProfiles, profile) {
		return rules.SingBoxRuleSet{}, fmt.Errorf("%w: %s", ErrUnknownProfile, profile)
	}

	// После неудачной проверки sing-box проверяется сразу: rule-set запрашивает запускающийся sing-box
	// (его бинарник могли заменить), и сейчас проверка пройдет, даже если он падает через секунду.
	if !m.support.ensure(ctx, 0).Enabled() {
		return rules.SingBoxRuleSet{
			Version: rules.RuleVersion,
			Rules:   []map[string]interface{}{},
		}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.data.ruleSet(profile), nil
}

// LookupIP возвращает MAC-адрес и имя устройства с адресом ip из последнего чтения таблицы соседей.
func (m *Manager) LookupIP(ip string) (string, string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, seen := range m.data.Seen {
		if !m.online[seen.MAC] || !slices.Contains(seen.IPs, ip) {
			continue
		}

		name := ""

		if index := m.indexLocked(seen.MAC); index >= 0 {
			name = m.data.Devices[index].Name
		}

		return seen.MAC, name, true
	}

	return "", "", false
}

// indexLocked возвращает индекс устройства с MAC-адресом mac или -1 (без блокировки).
func (m *Manager) indexLocked(mac string) int {
	return slices.IndexFunc(m.data.Devices, func(device Device) bool {
		return device.MAC == mac
	})
}

// saveLocked сохраняет раздел в app.json (без блокировки).
func (m *Manager) saveLocked() error {
	section := appDataSection{
		Devices: &m.data,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save devices: %w", err)
	}

	m.savedAt = time.Now()

	return nil
}
