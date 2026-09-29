// Package dnsconfig хранит DNS-настройки sing-box в app.json: серверы, общие параметры и пользовательские правила.
package dnsconfig

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
)

// Типы DNS-серверов sing-box, которые настраиваются через форму.
const (
	TypeLocal = "local"
	TypeUDP   = "udp"
	TypeTCP   = "tcp"
	TypeTLS   = "tls"
	TypeHTTPS = "https"
	TypeH3    = "h3"
	TypeQUIC  = "quic"
	TypeDHCP  = "dhcp"
)

// ReservedTagPrefix — префикс тегов, которые конфигуратор создает сам (например, configurer-hosts).
const ReservedTagPrefix = "configurer-"

var (
	// serverTypes — поддерживаемые типы серверов.
	serverTypes = []string{TypeLocal, TypeUDP, TypeTCP, TypeTLS, TypeHTTPS, TypeH3, TypeQUIC, TypeDHCP}

	// strategies — допустимые значения strategy.
	strategies = []string{"", "prefer_ipv4", "prefer_ipv6", "ipv4_only", "ipv6_only"}

	// tagRe — допустимый тег сервера.
	tagRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

	// reservedExtraKeys — поля секции dns, которые конфигуратор заполняет сам: в Extra их задавать нельзя,
	// иначе они молча перезапишут серверы, системные правила или параметры из формы.
	reservedExtraKeys = []string{"servers", "rules", "final", "strategy", "cache_capacity", "optimistic", "timeout"}
)

// ValidateExtra проверяет дополнительные поля секции dns: зарезервированные поля запрещены.
func ValidateExtra(extra map[string]any) error {
	for _, key := range reservedExtraKeys {
		if _, ok := extra[key]; ok {
			return fmt.Errorf("поле %q задается конфигуратором и не может быть в дополнительных полях секции dns", key)
		}
	}

	return nil
}

// Server — DNS-сервер sing-box. Поля Extra дописываются в объект сервера как есть: так задаются параметры,
// для которых нет полей формы (например, neighbor_domain форка sing-box-lx).
type Server struct {
	Tag            string         `json:"tag"`
	Type           string         `json:"type"`
	Server         string         `json:"server,omitempty"`
	ServerPort     int            `json:"server_port,omitempty"`
	Path           string         `json:"path,omitempty"`
	TLSServerName  string         `json:"tls_server_name,omitempty"`
	TLSInsecure    bool           `json:"tls_insecure,omitempty"`
	Detour         string         `json:"detour,omitempty"`
	DomainResolver string         `json:"domain_resolver,omitempty"`
	Description    string         `json:"description,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
}

// Settings — общие параметры DNS. Extra дописывается в секцию dns как есть.
type Settings struct {
	Final                 string         `json:"final"`
	Strategy              string         `json:"strategy,omitempty"`
	DefaultDomainResolver string         `json:"default_domain_resolver,omitempty"`
	CacheCapacity         int            `json:"cache_capacity,omitempty"`
	Optimistic            bool           `json:"optimistic,omitempty"`
	Timeout               string         `json:"timeout,omitempty"`
	Extra                 map[string]any `json:"extra,omitempty"`
}

// Data — раздел "dns" файла app.json.
type Data struct {
	Servers  []Server         `json:"servers"`
	Settings Settings         `json:"settings"`
	Rules    []map[string]any `json:"rules"`
}

// Default возвращает DNS-настройки новой инсталляции.
func Default() Data {
	return Data{
		Servers: []Server{
			{
				Tag:            "local",
				Type:           TypeLocal,
				Server:         "",
				ServerPort:     0,
				Path:           "",
				TLSServerName:  "",
				TLSInsecure:    false,
				Detour:         "",
				DomainResolver: "",
				Description:    "Системный DNS хоста",
				Extra:          nil,
			},
			{
				Tag:            "yandex",
				Type:           TypeUDP,
				Server:         "77.88.8.8",
				ServerPort:     0,
				Path:           "",
				TLSServerName:  "",
				TLSInsecure:    false,
				Detour:         "",
				DomainResolver: "",
				Description:    "Яндекс DNS",
				Extra:          nil,
			},
			{
				Tag:            "cloudflare",
				Type:           TypeHTTPS,
				Server:         "1.1.1.1",
				ServerPort:     0,
				Path:           "",
				TLSServerName:  "cloudflare-dns.com",
				TLSInsecure:    false,
				Detour:         "",
				DomainResolver: "",
				Description:    "Cloudflare DNS over HTTPS",
				Extra:          nil,
			},
		},
		Settings: Settings{
			Final:                 "yandex",
			Strategy:              "ipv4_only",
			DefaultDomainResolver: "yandex",
			CacheCapacity:         8192,
			Optimistic:            true,
			Timeout:               "5s",
			Extra:                 nil,
		},
		Rules: []map[string]any{},
	}
}

// usesTLS проверяет, что тип сервера работает поверх TLS.
func usesTLS(serverType string) bool {
	return serverType == TypeTLS || serverType == TypeHTTPS || serverType == TypeH3 || serverType == TypeQUIC
}

// needsAddress проверяет, что для типа сервера обязателен адрес.
func needsAddress(serverType string) bool {
	return serverType != TypeLocal && serverType != TypeDHCP
}

// Normalize обрезает пробелы в строковых полях сервера.
func (s *Server) Normalize() {
	s.Tag = strings.TrimSpace(s.Tag)
	s.Type = strings.TrimSpace(s.Type)
	s.Server = strings.TrimSpace(s.Server)
	s.Path = strings.TrimSpace(s.Path)
	s.TLSServerName = strings.TrimSpace(s.TLSServerName)
	s.Detour = strings.TrimSpace(s.Detour)
	s.DomainResolver = strings.TrimSpace(s.DomainResolver)
	s.Description = strings.TrimSpace(s.Description)

	if len(s.Extra) == 0 {
		s.Extra = nil
	}
}

// Validate проверяет поля сервера.
func (s *Server) Validate() error {
	if !tagRe.MatchString(s.Tag) {
		return fmt.Errorf("некорректный тег %q: допустимы латиница, цифры, точка, дефис и подчеркивание", s.Tag)
	}

	if strings.HasPrefix(s.Tag, ReservedTagPrefix) {
		return fmt.Errorf("теги с префиксом %s зарезервированы", ReservedTagPrefix)
	}

	if !slices.Contains(serverTypes, s.Type) {
		return fmt.Errorf("неподдерживаемый тип сервера %q", s.Type)
	}

	if needsAddress(s.Type) && s.Server == "" {
		return fmt.Errorf("для сервера типа %s нужен адрес", s.Type)
	}

	if s.ServerPort < 0 || s.ServerPort > 65535 {
		return fmt.Errorf("некорректный порт %d", s.ServerPort)
	}

	if s.DomainResolver == s.Tag {
		return fmt.Errorf("сервер не может резолвить свой адрес через себя")
	}

	return nil
}

// Config возвращает объект сервера для конфига sing-box.
func (s *Server) Config() map[string]any {
	result := map[string]any{
		"tag":  s.Tag,
		"type": s.Type,
	}

	if needsAddress(s.Type) {
		result["server"] = s.Server

		if s.ServerPort > 0 {
			result["server_port"] = s.ServerPort
		}
	}

	if s.Path != "" && (s.Type == TypeHTTPS || s.Type == TypeH3) {
		result["path"] = s.Path
	}

	if usesTLS(s.Type) && (s.TLSServerName != "" || s.TLSInsecure) {
		tls := map[string]any{}

		if s.TLSServerName != "" {
			tls["server_name"] = s.TLSServerName
		}

		if s.TLSInsecure {
			tls["insecure"] = true
		}

		result["tls"] = tls
	}

	if s.Detour != "" {
		result["detour"] = s.Detour
	}

	if s.DomainResolver != "" {
		result["domain_resolver"] = s.DomainResolver
	}

	jsonmap.Merge(result, s.Extra)

	return result
}

// ServerFromConfig разбирает объект DNS-сервера из конфига sing-box. Поля без отдельного места
// в форме попадают в Extra, поэтому Config восстанавливает исходный объект.
func ServerFromConfig(config map[string]any) Server {
	server := Server{
		Tag:            jsonmap.String(config, "tag"),
		Type:           jsonmap.String(config, "type"),
		Server:         jsonmap.String(config, "server"),
		ServerPort:     jsonmap.Int(config, "server_port"),
		Path:           jsonmap.String(config, "path"),
		TLSServerName:  "",
		TLSInsecure:    false,
		Detour:         jsonmap.String(config, "detour"),
		DomainResolver: jsonmap.String(config, "domain_resolver"),
		Description:    "",
		Extra:          jsonmap.Without(config, "tag", "type", "server", "server_port", "path", "detour", "domain_resolver", "tls"),
	}

	// domain_resolver может быть объектом с дополнительными параметрами — такой оставляем в Extra.
	if _, ok := config["domain_resolver"].(string); !ok && config["domain_resolver"] != nil {
		server.Extra["domain_resolver"] = jsonmap.Clone(config["domain_resolver"])
	}

	if tls, ok := config["tls"].(map[string]any); ok {
		server.TLSServerName = jsonmap.String(tls, "server_name")
		server.TLSInsecure = jsonmap.Bool(tls, "insecure")

		if rest := jsonmap.Without(tls, "server_name", "insecure", "enabled"); len(rest) > 0 {
			server.Extra["tls"] = rest
		}
	}

	// Путь по умолчанию у DoH и так /dns-query, но явное значение сохраняем как есть.
	if len(server.Extra) == 0 {
		server.Extra = nil
	}

	return server
}

// Normalize обрезает пробелы в строковых полях настроек.
func (s *Settings) Normalize() {
	s.Final = strings.TrimSpace(s.Final)
	s.Strategy = strings.TrimSpace(s.Strategy)
	s.DefaultDomainResolver = strings.TrimSpace(s.DefaultDomainResolver)
	s.Timeout = strings.TrimSpace(s.Timeout)

	if len(s.Extra) == 0 {
		s.Extra = nil
	}
}

// Validate проверяет настройки. servers — теги существующих серверов.
func (s *Settings) Validate(servers []string) error {
	if s.Final != "" && !slices.Contains(servers, s.Final) {
		return fmt.Errorf("сервер %q для final не найден", s.Final)
	}

	if s.DefaultDomainResolver != "" && !slices.Contains(servers, s.DefaultDomainResolver) {
		return fmt.Errorf("сервер %q для default_domain_resolver не найден", s.DefaultDomainResolver)
	}

	if !slices.Contains(strategies, s.Strategy) {
		return fmt.Errorf("некорректная стратегия %q", s.Strategy)
	}

	if s.CacheCapacity < 0 {
		return fmt.Errorf("размер кэша не может быть отрицательным")
	}

	if s.Timeout != "" {
		if _, err := time.ParseDuration(s.Timeout); err != nil {
			return fmt.Errorf("некорректный таймаут %q", s.Timeout)
		}
	}

	return ValidateExtra(s.Extra)
}

// Apply дописывает общие параметры в секцию dns конфига sing-box. Зарезервированные поля из Extra
// пропускаются: они могли остаться в данных, сохраненных до появления проверки.
func (s *Settings) Apply(dns map[string]any) {
	if s.Final != "" {
		dns["final"] = s.Final
	}

	if s.Strategy != "" {
		dns["strategy"] = s.Strategy
	}

	if s.CacheCapacity > 0 {
		dns["cache_capacity"] = s.CacheCapacity
	}

	if s.Optimistic {
		dns["optimistic"] = true
	}

	if s.Timeout != "" {
		dns["timeout"] = s.Timeout
	}

	jsonmap.Merge(dns, jsonmap.Without(s.Extra, reservedExtraKeys...))
}
