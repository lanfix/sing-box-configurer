package outbound

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const ShadowsocksProtocolName = "shadowsocks"

// shadowsocksMethods — методы шифрования, которые поддерживает sing-box.
var shadowsocksMethods = []string{
	"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
	"none", "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
	"aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
	"rc4-md5", "chacha20-ietf", "xchacha20",
}

// shadowsocksPlugins — плагины share-ссылок и их имена в sing-box.
var shadowsocksPlugins = map[string]string{
	"obfs-local":   "obfs-local",
	"simple-obfs":  "obfs-local",
	"v2ray-plugin": "v2ray-plugin",
}

type ShareShadowsocks struct {
	outbound Outbound
}

func (s *ShareShadowsocks) GetOutbound() *Outbound {
	return &s.outbound
}

type ShareShadowsocksProvider struct{}

func NewShareShadowsocksProvider() *ShareShadowsocksProvider {
	return &ShareShadowsocksProvider{}
}

// Parse разбирает ссылку ss:// в формате SIP002 (ss://userinfo@host:port/?plugin=...#имя, userinfo — base64
// от method:password или method:password как есть) и в старом формате ss://base64(method:password@host:port)#имя.
func (p *ShareShadowsocksProvider) Parse(shareData string) (Share, error) {
	shareData, profileName := cutProfileName(shareData)

	body, rawQuery, _ := strings.Cut(shareData, "?")

	// Старый формат: все до имени закодировано в base64.
	if !strings.Contains(body, "@") {
		decoded, err := decodeBase64(strings.TrimSuffix(body, "/"))
		if err != nil {
			return nil, fmt.Errorf("ссылка не в формате SIP002 и не раскодируется из base64: %w", err)
		}

		body = decoded
	}

	at := strings.LastIndex(body, "@")
	if at < 0 {
		return nil, fmt.Errorf("в ссылке нет адреса сервера")
	}

	method, password, err := parseShadowsocksUserInfo(body[:at])
	if err != nil {
		return nil, err
	}

	host, port, err := splitHostPort(strings.TrimSuffix(body[at+1:], "/"))
	if err != nil {
		return nil, err
	}

	if !slices.Contains(shadowsocksMethods, method) {
		return nil, fmt.Errorf("метод шифрования %q не поддерживается sing-box", method)
	}

	profileName = coalesce(profileName, fmt.Sprintf("ss-%s", host))

	config := map[string]any{
		"type":        ShadowsocksProtocolName,
		"tag":         profileName,
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
	}

	params, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("cannot parse share data params: %s", err)
	}

	if plugin := params.Get("plugin"); plugin != "" {
		name, opts, _ := strings.Cut(plugin, ";")

		pluginName, ok := shadowsocksPlugins[name]
		if !ok {
			return nil, fmt.Errorf("плагин %q не поддерживается sing-box (есть obfs-local и v2ray-plugin)", name)
		}

		config["plugin"] = pluginName

		if opts != "" {
			config["plugin_opts"] = opts
		}
	}

	return &ShareShadowsocks{
		outbound: Outbound{
			Tag:        profileName,
			Type:       ShadowsocksProtocolName,
			Server:     host,
			Port:       int(port),
			Config:     config,
			IsEndpoint: false,
		},
	}, nil
}

// parseShadowsocksUserInfo возвращает метод и пароль из userinfo ссылки: base64 от method:password
// или method:password в URL-кодировке (так передаются ключи методов 2022).
func parseShadowsocksUserInfo(userInfo string) (method, password string, err error) {
	if decoded, unescapeErr := url.PathUnescape(userInfo); unescapeErr == nil && strings.Contains(decoded, ":") {
		method, password, _ = strings.Cut(decoded, ":")
	} else {
		decoded, err = decodeBase64(userInfo)
		if err != nil {
			return "", "", fmt.Errorf("метод и пароль не раскодируются из base64: %w", err)
		}

		var ok bool

		if method, password, ok = strings.Cut(decoded, ":"); !ok {
			return "", "", fmt.Errorf("метод и пароль должны быть в виде method:password")
		}
	}

	method = strings.ToLower(strings.TrimSpace(method))

	if method == "" || password == "" {
		return "", "", fmt.Errorf("в ссылке нет метода шифрования или пароля")
	}

	return method, password, nil
}

// cutProfileName отрезает имя профиля (#имя) и раскодирует его из URL-кодировки.
func cutProfileName(shareData string) (string, string) {
	shareData, name, found := strings.Cut(shareData, "#")
	if !found {
		return shareData, ""
	}

	return shareData, unescapeProfileName(name)
}

// unescapeProfileName раскодирует имя профиля из URL-кодировки. Если раскодировать не удалось, имя остается как есть.
func unescapeProfileName(name string) string {
	if decoded, err := url.PathUnescape(name); err == nil {
		return strings.TrimSpace(decoded)
	}

	return strings.TrimSpace(name)
}

// splitHostPort разбирает адрес host:port (IPv6 — в квадратных скобках).
func splitHostPort(address string) (string, uint16, error) {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("некорректный адрес сервера %q: %w", address, err)
	}

	if host == "" {
		return "", 0, fmt.Errorf("host is required")
	}

	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil || port == 0 {
		return "", 0, fmt.Errorf("invalid port number")
	}

	return host, uint16(port), nil
}

// decodeBase64 раскодирует base64 в стандартном и URL-алфавите, с выравниванием и без.
func decodeBase64(value string) (string, error) {
	value = strings.TrimSpace(value)

	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return string(decoded), nil
		}
	}

	return "", fmt.Errorf("некорректный base64")
}
