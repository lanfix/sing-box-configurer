// Package amnezia импортирует конфигурации Amnezia VPN (ключи vpn://) в конфиг sing-box.
package amnezia

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/happ"
)

// ErrPremiumKey возвращается парсером для ключей Amnezia Premium: конфиг выдает шлюз по API.
var ErrPremiumKey = errors.New("это ключ Amnezia Premium: конфигурация запрашивается у шлюза Amnezia")

// configVersionGateway — config_version конфигураций сервиса Amnezia (Premium).
const configVersionGateway = 2

// export — формат конфигурации, которую Amnezia упаковывает в ключ vpn://.
type export struct {
	ConfigVersion    int               `json:"config_version"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	HostName         string            `json:"hostName"`
	DefaultContainer string            `json:"defaultContainer"`
	Containers       []json.RawMessage `json:"containers"`
	APIConfig        json.RawMessage   `json:"api_config"`
	AuthData         struct {
		APIKey string `json:"api_key"`
	} `json:"auth_data"`
}

// isPremiumKey проверяет, что это ключ подписки: в нем есть api_key, но нет конфигураций протоколов.
func (e *export) isPremiumKey() bool {
	return e.ConfigVersion == configVersionGateway && len(e.Containers) == 0 && e.AuthData.APIKey != ""
}

// Item — сгенерированный outbound или endpoint sing-box.
type Item struct {
	Tag      string         `json:"tag"`
	Name     string         `json:"name"`
	Protocol string         `json:"protocol"`
	Server   string         `json:"server"`
	Config   map[string]any `json:"config"`

	// RequiresAWG — для работы нужен sing-box с поддержкой AmneziaWG (обфускация WireGuard).
	RequiresAWG bool `json:"requires_awg"`
}

// Parsed — результат разбора ключа.
type Parsed struct {
	Description string
	Server      string
	Items       []Item
	Warnings    []string
}

// Соответствие параметров AmneziaWG из экспорта полям wireguard endpoint форка sing-box-lx.
var (
	awgIntFields = map[string]string{
		"Jc":   "jc",
		"Jmin": "jmin",
		"Jmax": "jmax",
		"S1":   "s1",
		"S2":   "s2",
		"S3":   "s3",
		"S4":   "s4",
	}

	// Значение — число или диапазон "min-max".
	awgRangeFields = map[string]string{
		"H1":                     "h1",
		"H2":                     "h2",
		"H3":                     "h3",
		"H4":                     "h4",
		"RekeyAfterTime":         "rekey_after_time",
		"RekeyTimeout":           "rekey_timeout",
		"RejectAfterTime":        "reject_after_time",
		"KeepaliveTimeout":       "keepalive_timeout",
		"MaxHandshakeAttempts":   "max_handshake_attempts",
		"ContentPaddingAddition": "content_padding_addition",
	}

	awgStringFields = map[string]string{
		"I1":                  "i1",
		"I2":                  "i2",
		"I3":                  "i3",
		"I4":                  "i4",
		"I5":                  "i5",
		"HeaderProtectionKey": "header_protection_key",
	}

	// Значение "on"/"off".
	awgBoolFields = map[string]string{
		"RandomTrailers": "random_trailers",
		"DisableCookies": "disable_cookies",
	}

	// Поля last_config, которые относятся к самому WireGuard, а не к обфускации.
	wireGuardFields = map[string]bool{
		"allowed_ips": true, "clientId": true, "client_ip": true, "client_priv_key": true, "client_pub_key": true,
		"config": true, "hostName": true, "mtu": true, "persistent_keep_alive": true, "port": true,
		"psk_key": true, "server_pub_key": true,
	}
)

// Parse разбирает ключ vpn:// и возвращает outbounds/endpoints sing-box с тегами вида "[profileName] протокол".
// Для ключа Amnezia Premium возвращает ErrPremiumKey.
func Parse(key, profileName string) (*Parsed, error) {
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}

	return parseExport(raw, profileName)
}

// parseExport разбирает распакованную конфигурацию Amnezia (из ключа или из ответа шлюза).
func parseExport(raw []byte, profileName string) (*Parsed, error) {
	var data export

	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("cannot parse config: %w", err)
	}

	if data.isPremiumKey() {
		return nil, ErrPremiumKey
	}

	if len(data.Containers) == 0 {
		return nil, fmt.Errorf("в конфигурации нет протоколов")
	}

	result := &Parsed{
		Description: data.Description,
		Server:      data.HostName,
		Items:       []Item{},
		Warnings:    []string{},
	}

	for _, rawContainer := range data.Containers {
		var container map[string]json.RawMessage

		if err := json.Unmarshal(rawContainer, &container); err != nil {
			return nil, fmt.Errorf("cannot parse container: %w", err)
		}

		var name string

		_ = json.Unmarshal(container["container"], &name)

		items, warnings, err := parseContainer(name, container, profileName, data.HostName)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", name, err))

			continue
		}

		result.Items = append(result.Items, items...)
		result.Warnings = append(result.Warnings, warnings...)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("нет поддерживаемых протоколов: %s", strings.Join(result.Warnings, "; "))
	}

	return result, nil
}

// parseContainer конвертирует один контейнер (протокол) Amnezia.
func parseContainer(name string, container map[string]json.RawMessage, profileName, host string) ([]Item, []string, error) {
	switch name {
	case "amnezia-awg", "amnezia-awg2":
		item, warnings, err := parseWireGuard(container["awg"], profileName, host, "AmneziaWG", true)
		if err != nil {
			return nil, nil, err
		}

		return []Item{item}, warnings, nil

	case "amnezia-wireguard":
		item, warnings, err := parseWireGuard(container["wireguard"], profileName, host, "WireGuard", false)
		if err != nil {
			return nil, nil, err
		}

		return []Item{item}, warnings, nil

	case "amnezia-xray":
		return parseXray(container["xray"], profileName)
	}

	return nil, nil, fmt.Errorf("протокол не поддерживается")
}

// parseWireGuard собирает wireguard endpoint из контейнера WireGuard/AmneziaWG.
func parseWireGuard(raw json.RawMessage, profileName, host, protocol string, obfuscated bool) (Item, []string, error) {
	var container map[string]any

	if err := json.Unmarshal(raw, &container); err != nil {
		return Item{}, nil, fmt.Errorf("cannot parse protocol config: %w", err)
	}

	// Параметры клиента лежат в last_config — JSON-строке внутри контейнера.
	lastConfigText, _ := container["last_config"].(string)

	var last map[string]any

	if err := json.Unmarshal([]byte(lastConfigText), &last); err != nil {
		return Item{}, nil, fmt.Errorf("нет данных клиента (last_config): %w", err)
	}

	privateKey := stringValue(last["client_priv_key"])
	publicKey := stringValue(last["server_pub_key"])
	address := stringValue(last["client_ip"])

	if privateKey == "" || publicKey == "" || address == "" {
		return Item{}, nil, fmt.Errorf("в конфиге нет ключей или адреса клиента")
	}

	if !strings.Contains(address, "/") {
		address += "/32"
	}

	server := coalesce(stringValue(last["hostName"]), host)

	port, err := strconv.Atoi(coalesce(stringValue(last["port"]), stringValue(container["port"])))
	if err != nil || server == "" {
		return Item{}, nil, fmt.Errorf("в конфиге нет адреса или порта сервера")
	}

	allowedIPs := []string{"0.0.0.0/0", "::/0"}

	if list, ok := last["allowed_ips"].([]any); ok && len(list) > 0 {
		allowedIPs = allowedIPs[:0]

		for _, value := range list {
			allowedIPs = append(allowedIPs, stringValue(value))
		}
	}

	peer := map[string]any{
		"address":     server,
		"port":        port,
		"public_key":  publicKey,
		"allowed_ips": allowedIPs,
	}

	if psk := stringValue(last["psk_key"]); psk != "" {
		peer["pre_shared_key"] = psk
	}

	if keepalive := stringValue(last["persistent_keep_alive"]); keepalive != "" {
		peer["persistent_keepalive_interval"] = numberOrRange(keepalive)
	}

	tag := fmt.Sprintf("[%s] %s", profileName, protocol)

	endpoint := map[string]any{
		"type":        "wireguard",
		"tag":         tag,
		"address":     []string{address},
		"private_key": privateKey,
		"peers":       []any{peer},
	}

	if mtu, err := strconv.Atoi(stringValue(last["mtu"])); err == nil && mtu > 0 {
		endpoint["mtu"] = mtu
	}

	warnings := []string{}

	if obfuscated {
		warnings = applyAWG(endpoint, last, container)
	}

	return Item{
		Tag:         tag,
		Name:        protocol,
		Protocol:    "wireguard",
		Server:      fmt.Sprintf("%s:%d", server, port),
		Config:      endpoint,
		RequiresAWG: obfuscated,
	}, warnings, nil
}

// applyAWG переносит параметры обфускации AmneziaWG в endpoint. Значения берутся из last_config,
// а при отсутствии — из самого контейнера. Возвращает предупреждения о неизвестных параметрах.
func applyAWG(endpoint, last, container map[string]any) []string {
	lookup := func(key string) string {
		return coalesce(stringValue(last[key]), stringValue(container[key]))
	}

	for from, to := range awgIntFields {
		if value, err := strconv.Atoi(lookup(from)); err == nil {
			endpoint[to] = value
		}
	}

	for from, to := range awgRangeFields {
		if value := lookup(from); value != "" {
			endpoint[to] = numberOrRange(value)
		}
	}

	for from, to := range awgStringFields {
		if value := lookup(from); value != "" {
			endpoint[to] = value
		}
	}

	for from, to := range awgBoolFields {
		if value := lookup(from); value != "" {
			endpoint[to] = value == "on" || value == "true" || value == "1"
		}
	}

	unknown := []string{}

	for key := range last {
		if wireGuardFields[key] || isMapped(key) {
			continue
		}

		unknown = append(unknown, key)
	}

	sort.Strings(unknown)

	if len(unknown) == 0 {
		return []string{}
	}

	return []string{"AmneziaWG: пропущены неизвестные параметры " + strings.Join(unknown, ", ")}
}

// parseXray конвертирует контейнер Xray через конвертер xray-конфигов подписок Happ.
func parseXray(raw json.RawMessage, profileName string) ([]Item, []string, error) {
	var container map[string]any

	if err := json.Unmarshal(raw, &container); err != nil {
		return nil, nil, fmt.Errorf("cannot parse protocol config: %w", err)
	}

	lastConfig, _ := container["last_config"].(string)
	if lastConfig == "" {
		return nil, nil, fmt.Errorf("нет данных клиента (last_config)")
	}

	parsed, err := happ.ParseSubscription([]byte(lastConfig), profileName)
	if err != nil {
		return nil, nil, err
	}

	items := make([]Item, 0, len(parsed.Servers))

	for _, server := range parsed.Servers {
		items = append(items, Item{
			Tag:         server.Tag,
			Name:        server.Name,
			Protocol:    server.Protocol,
			Server:      fmt.Sprintf("%s:%d", server.Address, server.Port),
			Config:      server.Outbound,
			RequiresAWG: false,
		})
	}

	return items, parsed.Warnings, nil
}

// decodeKey распаковывает ключ vpn://: base64url от qCompress (4 байта длины + zlib) или от JSON.
func decodeKey(key string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(strings.TrimSpace(key), "vpn://")
	if !ok {
		return nil, fmt.Errorf("ключ должен начинаться с vpn://")
	}

	return decodePayload(encoded)
}

// decodePayload распаковывает содержимое ключа без префикса vpn:// (шлюз отдает его и без префикса).
func decodePayload(encoded string) ([]byte, error) {
	encoded = strings.TrimPrefix(strings.TrimSpace(encoded), "vpn://")

	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(encoded, "="))
	if err != nil {
		return nil, fmt.Errorf("cannot decode key: %w", err)
	}

	if len(data) > 4 {
		if decompressed, err := inflate(data[4:]); err == nil {
			return decompressed, nil
		}
	}

	// Старые ключи могут содержать JSON без сжатия.
	if json.Valid(data) {
		return data, nil
	}

	return nil, fmt.Errorf("cannot decompress key")
}

// inflate распаковывает zlib-поток.
func inflate(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = reader.Close()
	}()

	return io.ReadAll(io.LimitReader(reader, 4<<20))
}

// isMapped проверяет, что параметр AmneziaWG переносится в endpoint.
func isMapped(key string) bool {
	for _, fields := range []map[string]string{awgIntFields, awgRangeFields, awgStringFields, awgBoolFields} {
		if _, ok := fields[key]; ok {
			return true
		}
	}

	return false
}

// numberOrRange возвращает число, если значение — одно число, иначе строку диапазона "min-max".
func numberOrRange(value string) any {
	if number, err := strconv.Atoi(value); err == nil {
		return number
	}

	return value
}

// stringValue приводит значение JSON к строке.
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)

	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}

	return ""
}

// coalesce возвращает первое непустое значение.
func coalesce(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
