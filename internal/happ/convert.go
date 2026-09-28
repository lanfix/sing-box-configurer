package happ

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
)

// Типы серверов профиля.
const (
	ServerTypeProxy   = "proxy"
	ServerTypeURLTest = "urltest"
)

// Server описывает outbound sing-box, полученный из подписки.
type Server struct {
	Tag      string         `json:"tag"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Protocol string         `json:"protocol"`
	Address  string         `json:"address"`
	Port     int            `json:"port"`
	Outbound map[string]any `json:"outbound"`
}

// ParseResult содержит результат разбора подписки.
type ParseResult struct {
	Servers  []Server
	Warnings []string
}

// xrayConfig описывает нужную часть полного конфига xray из подписки.
type xrayConfig struct {
	Remarks   string         `json:"remarks"`
	Outbounds []xrayOutbound `json:"outbounds"`
	Routing   *struct {
		Balancers []struct {
			Tag      string   `json:"tag"`
			Selector []string `json:"selector"`
		} `json:"balancers"`
	} `json:"routing"`
}

type xrayOutbound struct {
	Tag            string          `json:"tag"`
	Protocol       string          `json:"protocol"`
	Settings       json.RawMessage `json:"settings"`
	StreamSettings *xrayStream     `json:"streamSettings"`
}

type xraySettings struct {
	Vnext []struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
		Users   []struct {
			ID       string `json:"id"`
			Flow     string `json:"flow"`
			Security string `json:"security"`
			AlterID  int    `json:"alterId"`
		} `json:"users"`
	} `json:"vnext"`
	Servers []struct {
		Address  string `json:"address"`
		Port     int    `json:"port"`
		Password string `json:"password"`
		Method   string `json:"method"`
	} `json:"servers"`
}

type xrayStream struct {
	Network     string `json:"network"`
	Security    string `json:"security"`
	TLSSettings *struct {
		ServerName    string   `json:"serverName"`
		Fingerprint   string   `json:"fingerprint"`
		ALPN          []string `json:"alpn"`
		AllowInsecure bool     `json:"allowInsecure"`
	} `json:"tlsSettings"`
	RealitySettings *struct {
		ServerName  string `json:"serverName"`
		PublicKey   string `json:"publicKey"`
		ShortID     string `json:"shortId"`
		Fingerprint string `json:"fingerprint"`
	} `json:"realitySettings"`
	TCPSettings *struct {
		Header *struct {
			Type string `json:"type"`
		} `json:"header"`
	} `json:"tcpSettings"`
	WSSettings *struct {
		Path    string            `json:"path"`
		Host    string            `json:"host"`
		Headers map[string]string `json:"headers"`
	} `json:"wsSettings"`
	GRPCSettings *struct {
		ServiceName string `json:"serviceName"`
	} `json:"grpcSettings"`
	HTTPUpgradeSettings *struct {
		Path string `json:"path"`
		Host string `json:"host"`
	} `json:"httpupgradeSettings"`
}

// convertedProxy содержит сконвертированный outbound и его источник.
type convertedProxy struct {
	xrayTag  string
	identity string
	server   Server
}

// ParseSubscription разбирает тело подписки (xray JSON, base64 или список share-ссылок)
// и возвращает серверы с тегами, уникальными в рамках профиля profileName.
func ParseSubscription(body []byte, profileName string) (*ParseResult, error) {
	trimmed := bytes.TrimSpace(body)

	builder := newServerBuilder(profileName)

	switch {
	case bytes.HasPrefix(trimmed, []byte("[")):
		var configs []xrayConfig

		if err := json.Unmarshal(trimmed, &configs); err != nil {
			return nil, fmt.Errorf("cannot parse xray config list: %w", err)
		}

		builder.addXrayConfigs(configs)

	case bytes.HasPrefix(trimmed, []byte("{")):
		var config xrayConfig

		if err := json.Unmarshal(trimmed, &config); err != nil {
			return nil, fmt.Errorf("cannot parse xray config: %w", err)
		}

		builder.addXrayConfigs([]xrayConfig{config})

	default:
		builder.addShareLinks(decodeShareLinks(trimmed))
	}

	if len(builder.servers) == 0 {
		return nil, fmt.Errorf("no supported servers in subscription")
	}

	return &ParseResult{
		Servers:  builder.servers,
		Warnings: builder.warnings,
	}, nil
}

// serverBuilder накапливает серверы, убирая дубликаты и обеспечивая уникальность тегов.
type serverBuilder struct {
	profileName string
	servers     []Server
	warnings    []string
	byIdentity  map[string]string
	usedNames   map[string]bool
}

func newServerBuilder(profileName string) *serverBuilder {
	return &serverBuilder{
		profileName: profileName,
		servers:     []Server{},
		warnings:    []string{},
		byIdentity:  map[string]string{},
		usedNames:   map[string]bool{},
	}
}

// addXrayConfigs добавляет серверы из списка конфигов xray.
// Сначала обрабатываются конфиги с одним сервером, чтобы серверы получили «человеческие» имена,
// затем конфиги с балансировщиками, которые превращаются в urltest.
func (b *serverBuilder) addXrayConfigs(configs []xrayConfig) {
	converted := make([][]convertedProxy, len(configs))

	for i := range configs {
		converted[i] = b.convertProxies(configs[i])
	}

	for i := range configs {
		if len(converted[i]) != 1 {
			continue
		}

		b.addProxy(converted[i][0], configs[i].Remarks)
	}

	for i := range configs {
		proxies := converted[i]
		if len(proxies) <= 1 {
			continue
		}

		for j := range proxies {
			b.addProxy(proxies[j], proxies[j].server.Address)
		}

		if configs[i].Routing == nil || len(configs[i].Routing.Balancers) == 0 {
			continue
		}

		members := make([]string, 0, len(proxies))

		for _, balancer := range configs[i].Routing.Balancers {
			for j := range proxies {
				if !matchesSelector(proxies[j].xrayTag, balancer.Selector) {
					continue
				}

				tag := b.byIdentity[proxies[j].identity]
				if !slices.Contains(members, tag) {
					members = append(members, tag)
				}
			}
		}

		if len(members) > 1 {
			b.addURLTest(configs[i].Remarks, members)
		}
	}
}

// convertProxies конвертирует proxy-outbounds конфига xray в формат sing-box.
func (b *serverBuilder) convertProxies(config xrayConfig) []convertedProxy {
	result := make([]convertedProxy, 0, len(config.Outbounds))

	for _, xo := range config.Outbounds {
		switch xo.Protocol {
		case "vless", "vmess", "trojan", "shadowsocks":

		default:
			continue
		}

		server, err := convertXrayOutbound(xo)
		if err != nil {
			b.warnings = append(b.warnings, fmt.Sprintf("%s: %s", config.Remarks, err))

			continue
		}

		// Заглушки панели (например, при превышении лимита устройств) ведут на .invalid домены.
		if strings.HasSuffix(server.Address, ".invalid") {
			continue
		}

		result = append(result, convertedProxy{
			xrayTag:  xo.Tag,
			identity: outboundIdentity(server.Outbound),
			server:   server,
		})
	}

	return result
}

// addShareLinks добавляет серверы из share-ссылок.
func (b *serverBuilder) addShareLinks(links []string) {
	for _, link := range links {
		share, err := outbound.ParseShareUrl(link)
		if err != nil {
			b.warnings = append(b.warnings, fmt.Sprintf("%s: %s", truncate(link, 32), err))

			continue
		}

		parsed := share.GetOutbound()

		if parsed.IsEndpoint {
			b.warnings = append(b.warnings, fmt.Sprintf("%s: endpoints are not supported", parsed.Tag))

			continue
		}

		config := make(map[string]any, len(parsed.Config))

		for key, value := range parsed.Config {
			if key != "tag" {
				config[key] = value
			}
		}

		b.addProxy(convertedProxy{
			xrayTag:  "",
			identity: outboundIdentity(config),
			server: Server{
				Tag:      "",
				Name:     "",
				Type:     ServerTypeProxy,
				Protocol: parsed.Type,
				Address:  parsed.Server,
				Port:     parsed.Port,
				Outbound: config,
			},
		}, parsed.Tag)
	}
}

// addProxy добавляет сервер, если такого ещё нет.
func (b *serverBuilder) addProxy(proxy convertedProxy, name string) {
	if _, ok := b.byIdentity[proxy.identity]; ok {
		return
	}

	server := proxy.server
	server.Name = b.uniqueName(coalesce(strings.TrimSpace(name), server.Address))
	server.Tag = b.tag(server.Name)
	server.Outbound["tag"] = server.Tag

	b.byIdentity[proxy.identity] = server.Tag
	b.servers = append(b.servers, server)
}

// addURLTest добавляет группу автоматического выбора самого быстрого сервера.
func (b *serverBuilder) addURLTest(name string, members []string) {
	name = b.uniqueName(coalesce(strings.TrimSpace(name), "Auto"))
	tag := b.tag(name)

	b.servers = append(b.servers, Server{
		Tag:      tag,
		Name:     name,
		Type:     ServerTypeURLTest,
		Protocol: "urltest",
		Address:  "",
		Port:     0,
		Outbound: map[string]any{
			"type":      "urltest",
			"tag":       tag,
			"outbounds": members,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "3m",
		},
	})
}

// uniqueName возвращает имя, не совпадающее с уже использованными в профиле.
func (b *serverBuilder) uniqueName(name string) string {
	candidate := name

	for i := 2; b.usedNames[candidate]; i++ {
		candidate = fmt.Sprintf("%s %d", name, i)
	}

	b.usedNames[candidate] = true

	return candidate
}

// tag формирует тег outbound с префиксом профиля.
// Префикс в квадратных скобках не пересекается с системными тегами select-*.
func (b *serverBuilder) tag(name string) string {
	return fmt.Sprintf("[%s] %s", b.profileName, name)
}

// convertXrayOutbound конвертирует outbound xray в outbound sing-box (без тега).
func convertXrayOutbound(xo xrayOutbound) (Server, error) {
	var settings xraySettings

	if err := json.Unmarshal(xo.Settings, &settings); err != nil {
		return Server{}, fmt.Errorf("cannot parse settings: %w", err)
	}

	config := map[string]any{
		"type": xo.Protocol,
	}

	var (
		address string
		port    int
	)

	switch xo.Protocol {
	case "vless", "vmess":
		if len(settings.Vnext) == 0 || len(settings.Vnext[0].Users) == 0 {
			return Server{}, fmt.Errorf("%s: vnext is empty", xo.Protocol)
		}

		vnext := settings.Vnext[0]
		user := vnext.Users[0]
		address, port = vnext.Address, vnext.Port

		config["uuid"] = user.ID

		if xo.Protocol == "vless" {
			config["packet_encoding"] = "xudp"

			if user.Flow != "" {
				config["flow"] = user.Flow
			}
		} else {
			config["security"] = coalesce(user.Security, "auto")
			config["alter_id"] = user.AlterID
		}

	case "trojan", "shadowsocks":
		if len(settings.Servers) == 0 {
			return Server{}, fmt.Errorf("%s: servers is empty", xo.Protocol)
		}

		srv := settings.Servers[0]
		address, port = srv.Address, srv.Port

		config["password"] = srv.Password

		if xo.Protocol == "shadowsocks" {
			config["method"] = srv.Method
		}
	}

	config["server"] = address
	config["server_port"] = port

	if xo.StreamSettings != nil {
		if err := applyXrayStream(config, xo.StreamSettings); err != nil {
			return Server{}, fmt.Errorf("%s:%d: %w", address, port, err)
		}
	}

	return Server{
		Tag:      "",
		Name:     "",
		Type:     ServerTypeProxy,
		Protocol: xo.Protocol,
		Address:  address,
		Port:     port,
		Outbound: config,
	}, nil
}

// applyXrayStream переносит транспорт и TLS из streamSettings xray в outbound sing-box.
func applyXrayStream(config map[string]any, stream *xrayStream) error {
	switch coalesce(stream.Network, "tcp") {
	case "tcp", "raw":
		if stream.TCPSettings != nil && stream.TCPSettings.Header != nil &&
			stream.TCPSettings.Header.Type != "" && stream.TCPSettings.Header.Type != "none" {
			return fmt.Errorf("tcp header %q is not supported", stream.TCPSettings.Header.Type)
		}

	case "ws":
		transport := map[string]any{
			"type": "ws",
		}

		if ws := stream.WSSettings; ws != nil {
			path, earlyData := splitEarlyData(ws.Path)
			transport["path"] = coalesce(path, "/")

			if host := coalesce(ws.Host, ws.Headers["Host"]); host != "" {
				transport["headers"] = map[string]any{
					"Host": host,
				}
			}

			if earlyData > 0 {
				transport["max_early_data"] = earlyData
				transport["early_data_header_name"] = "Sec-WebSocket-Protocol"
			}
		}

		config["transport"] = transport

	case "grpc":
		transport := map[string]any{
			"type": "grpc",
		}

		if grpc := stream.GRPCSettings; grpc != nil && grpc.ServiceName != "" {
			transport["service_name"] = grpc.ServiceName
		}

		config["transport"] = transport

	case "httpupgrade":
		transport := map[string]any{
			"type": "httpupgrade",
		}

		if hu := stream.HTTPUpgradeSettings; hu != nil {
			transport["path"] = coalesce(hu.Path, "/")

			if hu.Host != "" {
				transport["host"] = hu.Host
			}
		}

		config["transport"] = transport

	default:
		return fmt.Errorf("transport %q is not supported by sing-box", stream.Network)
	}

	switch stream.Security {
	case "", "none":

	case "tls":
		tls := map[string]any{
			"enabled": true,
		}

		if ts := stream.TLSSettings; ts != nil {
			if ts.ServerName != "" {
				tls["server_name"] = ts.ServerName
			}

			if len(ts.ALPN) > 0 {
				tls["alpn"] = ts.ALPN
			}

			if ts.AllowInsecure {
				tls["insecure"] = true
			}

			if ts.Fingerprint != "" {
				tls["utls"] = map[string]any{
					"enabled":     true,
					"fingerprint": ts.Fingerprint,
				}
			}
		}

		config["tls"] = tls

	case "reality":
		rs := stream.RealitySettings
		if rs == nil || rs.PublicKey == "" {
			return fmt.Errorf("reality public key is required")
		}

		// Reality в sing-box работает только через uTLS.
		config["tls"] = map[string]any{
			"enabled":     true,
			"server_name": rs.ServerName,
			"utls": map[string]any{
				"enabled":     true,
				"fingerprint": coalesce(rs.Fingerprint, "chrome"),
			},
			"reality": map[string]any{
				"enabled":    true,
				"public_key": rs.PublicKey,
				"short_id":   rs.ShortID,
			},
		}

	default:
		return fmt.Errorf("security %q is not supported", stream.Security)
	}

	return nil
}

// splitEarlyData отделяет параметр ed (early data) от пути websocket.
func splitEarlyData(path string) (string, int) {
	base, query, ok := strings.Cut(path, "?")
	if !ok {
		return path, 0
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		return path, 0
	}

	earlyData, err := strconv.Atoi(values.Get("ed"))
	if err != nil {
		return path, 0
	}

	values.Del("ed")

	if encoded := values.Encode(); encoded != "" {
		return base + "?" + encoded, earlyData
	}

	return base, earlyData
}

// decodeShareLinks извлекает share-ссылки из тела подписки в виде base64 или обычного текста.
func decodeShareLinks(body []byte) []string {
	text := string(body)

	if !strings.Contains(text, "://") {
		compact := strings.Join(strings.Fields(text), "")

		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if decoded, err := encoding.DecodeString(compact); err == nil {
				text = string(decoded)

				break
			}
		}
	}

	links := make([]string, 0)

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "://") {
			links = append(links, line)
		}
	}

	return links
}

// matchesSelector проверяет, что тег подходит под селектор балансировщика xray (сравнение по префиксу).
func matchesSelector(tag string, selectors []string) bool {
	for _, selector := range selectors {
		if strings.HasPrefix(tag, selector) {
			return true
		}
	}

	return false
}

// outboundIdentity возвращает ключ для поиска одинаковых серверов (без учета тега).
func outboundIdentity(config map[string]any) string {
	withoutTag := make(map[string]any, len(config))

	for key, value := range config {
		if key != "tag" {
			withoutTag[key] = value
		}
	}

	// encoding/json сортирует ключи map, поэтому результат детерминирован.
	raw, _ := json.Marshal(withoutTag)

	return string(raw)
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
