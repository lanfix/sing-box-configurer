package settings

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// hostnameRe — доменное имя или шаблон *.домен: буквы, цифры, дефисы и точки.
var hostnameRe = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// Security — защита панели от запросов с чужих сайтов.
type Security struct {
	// CheckHost — принимать запросы, только если панель открыта по IP-адресу, localhost или разрешенному
	// доменному имени. Защищает от DNS rebinding: чужой домен, который начал указывать на адрес панели,
	// не получит ответа.
	CheckHost bool `json:"check_host"`

	// AllowedHosts — доменные имена, по которым открывается панель. *.example.com разрешает поддомены.
	AllowedHosts []string `json:"allowed_hosts"`
}

// DefaultSecurity возвращает защиту по умолчанию: проверка адреса включена, домены не разрешены.
func DefaultSecurity() Security {
	return Security{
		CheckHost:    true,
		AllowedHosts: []string{},
	}
}

// Normalize приводит доменные имена к нижнему регистру без порта и повторов и проверяет их.
func (s Security) Normalize() (Security, error) {
	hosts := make([]string, 0, len(s.AllowedHosts))

	for _, raw := range s.AllowedHosts {
		host := Hostname(raw)

		if host == "" || slices.Contains(hosts, host) {
			continue
		}

		// Адрес со схемой или путем (http://router.lan/) — не имя хоста: SplitHostPort принял бы «http» за имя.
		if strings.ContainsAny(raw, "/@") || !hostnameRe.MatchString(host) {
			return Security{}, fmt.Errorf("%q не является доменным именем: укажите имя без http:// и пути, например router.lan", strings.TrimSpace(raw))
		}

		hosts = append(hosts, host)
	}

	return Security{
		CheckHost:    s.CheckHost,
		AllowedHosts: hosts,
	}, nil
}

// HostAllowed проверяет, что по адресу host (значение заголовка Host, можно с портом) панель открывается:
// проверка выключена, это IP-адрес, localhost, имя из extra (адреса из конфига сервиса) или разрешенный домен.
func (s Security) HostAllowed(host string, extra []string) bool {
	if !s.CheckHost {
		return true
	}

	name := Hostname(host)

	// Без заголовка Host (HTTP/1.0) запрос не может прийти со страницы чужого сайта.
	if name == "" || name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return true
	}

	if _, err := netip.ParseAddr(name); err == nil {
		return true
	}

	for _, allowed := range append(slices.Clone(s.AllowedHosts), extra...) {
		allowed = Hostname(allowed)

		if suffix, ok := strings.CutPrefix(allowed, "*."); ok {
			if strings.HasSuffix(name, "."+suffix) {
				return true
			}

			continue
		}

		if name == allowed {
			return true
		}
	}

	return false
}

// Hostname возвращает имя хоста из host: без порта, квадратных скобок IPv6, точки в конце и в нижнем регистре.
func Hostname(host string) string {
	host = strings.TrimSpace(host)

	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}

	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")

	return strings.TrimSuffix(strings.ToLower(host), ".")
}
