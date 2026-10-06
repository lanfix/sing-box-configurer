// Package devices хранит в app.json профили устройств LAN по MAC-адресам и отдает sing-box rule-set-ы
// профилей: устройство можно оставить с обходом блокировок, пустить напрямую или отключить от интернета,
// а неизвестным устройствам задать профиль по умолчанию. Списки меняются без перезапуска sing-box.
package devices

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Профили устройств.
const (
	// ProfileDefault — устройство следует профилю по умолчанию (как неизвестное).
	ProfileDefault = "default"

	// ProfileProxy — обычная маршрутизация по группам, с обходом блокировок.
	ProfileProxy = "proxy"

	// ProfileDirect — весь трафик напрямую, как у провайдера.
	ProfileDirect = "direct"

	// ProfileBlocked — без интернета: соединения и DNS-запросы отклоняются.
	ProfileBlocked = "blocked"
)

// maxNameLength ограничивает длину имени устройства.
const maxNameLength = 64

var (
	// ErrNotFound — устройства с таким MAC нет.
	ErrNotFound = errors.New("device not found")

	// RuleSetProfiles — профили, для которых sing-box получает rule-set-ы.
	RuleSetProfiles = []string{ProfileDirect, ProfileBlocked}
)

// Device — устройство с профилем, заданным вручную.
type Device struct {
	MAC     string    `json:"mac"`
	Name    string    `json:"name"`
	Profile string    `json:"profile"`
	AddedAt time.Time `json:"added_at"`
}

// Settings — политика для устройств без своего профиля.
type Settings struct {
	// DefaultProfile — профиль неизвестных устройств: proxy, direct или blocked.
	DefaultProfile string `json:"default_profile"`

	// AutoNetworks — сети LAN определяются по таблице соседей хоста.
	AutoNetworks bool `json:"auto_networks"`

	// Networks — сети LAN, заданные вручную (дополняют найденные автоматически).
	Networks []string `json:"networks"`

	// Exclude — адреса и подсети, к которым профиль по умолчанию не применяется (VPN-клиенты, серверы).
	Exclude []string `json:"exclude"`

	// DirectDNSServer — DNS-сервер для устройств без обхода (пусто — общие DNS-правила).
	DirectDNSServer string `json:"direct_dns_server"`
}

// Detected — последние найденные сети LAN и адреса хоста. Хранятся, чтобы политика не пропадала,
// пока сеть хоста недоступна (например, sing-box перезапускается).
type Detected struct {
	Networks      []string  `json:"networks"`
	HostAddresses []string  `json:"host_addresses"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Seen — устройство, найденное в таблице соседей хоста.
type Seen struct {
	MAC       string    `json:"mac"`
	IPs       []string  `json:"ips"`
	Interface string    `json:"interface"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Data — раздел "devices" файла app.json.
type Data struct {
	Devices  []Device `json:"devices"`
	Settings Settings `json:"settings"`
	Detected Detected `json:"detected"`
	Seen     []Seen   `json:"seen"`
}

// DefaultSettings возвращает настройки новой инсталляции: неизвестные устройства с обходом, как раньше.
func DefaultSettings() Settings {
	return Settings{
		DefaultProfile:  ProfileProxy,
		AutoNetworks:    true,
		Networks:        []string{},
		Exclude:         []string{},
		DirectDNSServer: "",
	}
}

// normalize обрезает пробелы и проверяет поля устройства.
func (d *Device) normalize() error {
	mac := NormalizeMAC(d.MAC)
	if mac == "" {
		return fmt.Errorf("некорректный MAC-адрес %q", d.MAC)
	}

	d.MAC = mac
	d.Name = strings.TrimSpace(d.Name)

	if utf8.RuneCountInString(d.Name) > maxNameLength {
		return fmt.Errorf("имя длиннее %d символов", maxNameLength)
	}

	if !slices.Contains([]string{ProfileDefault, ProfileProxy, ProfileDirect, ProfileBlocked}, d.Profile) {
		return fmt.Errorf("неизвестный профиль %q", d.Profile)
	}

	return nil
}

// normalize проверяет настройки и приводит адреса к виду подсетей.
func (s *Settings) normalize() error {
	if !slices.Contains([]string{ProfileProxy, ProfileDirect, ProfileBlocked}, s.DefaultProfile) {
		return fmt.Errorf("неизвестный профиль по умолчанию %q", s.DefaultProfile)
	}

	networks, err := normalizePrefixes(s.Networks)
	if err != nil {
		return fmt.Errorf("сети LAN: %w", err)
	}

	exclude, err := normalizePrefixes(s.Exclude)
	if err != nil {
		return fmt.Errorf("исключения: %w", err)
	}

	s.Networks = networks
	s.Exclude = exclude
	s.DirectDNSServer = strings.TrimSpace(s.DirectDNSServer)

	return nil
}

// normalizePrefixes приводит адреса и подсети к виду подсетей с обнуленными битами хоста (адрес — /32 или /128).
func normalizePrefixes(values []string) ([]string, error) {
	result := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)

		if value == "" {
			continue
		}

		prefix, err := parsePrefix(value)
		if err != nil {
			return nil, err
		}

		result = appendUnique(result, prefix.String())
	}

	return result, nil
}

// parsePrefix разбирает подсеть или одиночный адрес.
func parsePrefix(value string) (netip.Prefix, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("некорректная подсеть %q", value)
		}

		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("некорректный адрес %q", value)
	}

	return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), nil
}
