// Package security защищает панель от запросов с чужих сайтов: проверяет адрес, по которому открыта панель
// (DNS rebinding), отклоняет запросы с других сайтов к API и интерфейсу (CSRF и чтение данных) и задает
// заголовки безопасности (CSP, запрет встраивания во фрейм).
package security

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// contentSecurityPolicy разрешает интерфейсу только собственные скрипты: даже если в страницу попадет
// чужая разметка, скрипты из нее и ссылки javascript: не выполнятся. Встроенные стили нужны CodeMirror и Vue Flow.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; " +
	"form-action 'self'; frame-ancestors 'none'"

// Guard — защита панели от запросов с чужих сайтов.
type Guard struct {
	policy func() settings.Security

	// extraHosts — имена хостов из конфига сервиса (адрес rule-set-ов для sing-box и т.п.), разрешенные всегда.
	extraHosts []string
}

// NewGuard создает защиту. policy возвращает текущие настройки, extraHosts — адреса, по которым к конфигуратору
// обращаются sing-box и сам сервис.
func NewGuard(policy func() settings.Security, extraHosts []string) *Guard {
	hosts := make([]string, 0, len(extraHosts))

	// IP-адреса и localhost разрешены всегда, в списке остаются только доменные имена.
	all := settings.Security{
		CheckHost:    true,
		AllowedHosts: []string{},
	}

	for _, host := range extraHosts {
		if name := settings.Hostname(host); !all.HostAllowed(name, nil) && !slices.Contains(hosts, name) {
			hosts = append(hosts, name)
		}
	}

	return &Guard{
		policy:     policy,
		extraHosts: hosts,
	}
}

// ExtraHosts возвращает имена хостов из конфига сервиса, разрешенные всегда.
func (g *Guard) ExtraHosts() []string {
	return slices.Clone(g.extraHosts)
}

// HostAllowed проверяет, что панель можно открыть по адресу host при настройках security.
func (g *Guard) HostAllowed(security settings.Security, host string) bool {
	return security.HostAllowed(host, g.extraHosts)
}

// Handler пропускает к next только запросы, которые пришли по разрешенному адресу и не с чужого сайта.
// Запросы не из браузера (sing-box, updater, curl) не содержат заголовков Sec-Fetch-* и Origin и проходят.
func (g *Guard) Handler(next http.Handler) http.Handler {
	csrf := http.NewCrossOriginProtection()

	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deny(w, r, "Запрос с другого сайта отклонен")
	}))

	protected := csrf.Handler(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setHeaders(w.Header())

		if !g.HostAllowed(g.policy(), r.Host) {
			deny(w, r, fmt.Sprintf("Панель открыта по адресу %s, которого нет среди разрешенных доменов. "+
				"Откройте панель по IP-адресу и добавьте домен в «Система → Безопасность» или выключите проверку "+
				"адреса командой sing-box-configurer security reset.", settings.Hostname(r.Host)))

			return
		}

		if crossSite(r) {
			deny(w, r, "Запрос с другого сайта отклонен")

			return
		}

		protected.ServeHTTP(w, r)
	})
}

// setHeaders задает заголовки безопасности ответа.
func setHeaders(header http.Header) {
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Frame-Options", "DENY")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "same-origin")

	// Другие сайты не могут подключить ответы панели как картинку, скрипт или стиль.
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
}

// crossSite проверяет, что запрос отправила страница другого сайта. Переход пользователя по ссылке
// на страницу интерфейса разрешен, к API — нет.
func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return isAPI(r) || !userNavigation(r)

	case "same-origin", "none":
		return false
	}

	// Браузеры без Sec-Fetch-Site: источник запроса сравнивается с адресом панели.
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return true
	}

	return !strings.EqualFold(parsed.Host, r.Host)
}

// userNavigation проверяет, что запрос — открытие страницы в окне браузера (переход по ссылке).
func userNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
}

// isAPI проверяет, что запрос к API. Проверяются раскодированный и экранированный пути, как в auth.Middleware.
func isAPI(r *http.Request) bool {
	for _, value := range []string{r.URL.Path, r.URL.EscapedPath()} {
		if cleaned := path.Clean("/" + value); cleaned == "/api" || strings.HasPrefix(cleaned, "/api/") {
			return true
		}
	}

	return false
}

// deny отклоняет запрос: API получает ошибку в JSON, интерфейс — страницу с пояснением.
func deny(w http.ResponseWriter, r *http.Request, message string) {
	if isAPI(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": message,
		})

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)

	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>Доступ запрещен</title><p>%s</p>", html.EscapeString(message))
}
