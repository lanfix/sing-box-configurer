package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// newHandler возвращает защищенный обработчик с настройками security.
func newHandler(security settings.Security) http.Handler {
	guard := NewGuard(func() settings.Security {
		return security
	}, []string{"configurer"})

	return guard.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// TestCrossSite проверяет, что запросы с чужих сайтов к API и интерфейсу отклоняются, а свои — нет.
func TestCrossSite(t *testing.T) {
	handler := newHandler(settings.DefaultSecurity())

	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{name: "cross-site api get", method: http.MethodGet, path: "/api/config", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors"}, want: http.StatusForbidden},
		{name: "cross-site api post", method: http.MethodPost, path: "/api/apply", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors"}, want: http.StatusForbidden},
		{name: "same-site api get", method: http.MethodGet, path: "/api/config", headers: map[string]string{"Sec-Fetch-Site": "same-site"}, want: http.StatusForbidden},
		{name: "cross-site api navigation", method: http.MethodGet, path: "/api/config", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, want: http.StatusForbidden},
		{name: "cross-site script", method: http.MethodGet, path: "/assets/index.js", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "script"}, want: http.StatusForbidden},
		{name: "cross-site iframe", method: http.MethodGet, path: "/", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, want: http.StatusForbidden},
		{name: "cross-site form post", method: http.MethodPost, path: "/", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, want: http.StatusForbidden},
		{name: "foreign origin post", method: http.MethodPost, path: "/api/apply", headers: map[string]string{"Origin": "http://evil.example"}, want: http.StatusForbidden},
		{name: "link to ui", method: http.MethodGet, path: "/rules", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, want: http.StatusOK},
		{name: "same-origin api", method: http.MethodPost, path: "/api/apply", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, want: http.StatusOK},
		{name: "typed address", method: http.MethodGet, path: "/", headers: map[string]string{"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate"}, want: http.StatusOK},
		{name: "own origin post", method: http.MethodPost, path: "/api/apply", headers: map[string]string{"Origin": "http://192.168.1.1:8080"}, want: http.StatusOK},
		{name: "non-browser", method: http.MethodGet, path: "/api/ruleset/domain", headers: map[string]string{}, want: http.StatusOK},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://192.168.1.1:8080"+tc.path, nil)

		for key, value := range tc.headers {
			r.Header.Set(key, value)
		}

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != tc.want {
			t.Errorf("%s: code = %d, want %d", tc.name, w.Code, tc.want)
		}

		if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: security headers are not set", tc.name)
		}
	}
}

// TestHostCheck проверяет защиту от DNS rebinding: чужие домены отклоняются, IP и разрешенные домены — нет.
func TestHostCheck(t *testing.T) {
	security := settings.Security{
		CheckHost:    true,
		AllowedHosts: []string{"router.lan", "*.example.com"},
	}

	cases := map[string]int{
		"192.168.1.1:8080":         http.StatusOK,
		"[fd00::1]:8080":           http.StatusOK,
		"localhost:8080":           http.StatusOK,
		"router.lan:8080":          http.StatusOK,
		"ROUTER.LAN.":              http.StatusOK,
		"panel.example.com":        http.StatusOK,
		"configurer:8080":          http.StatusOK,
		"example.com":              http.StatusForbidden,
		"attacker.example.net:80":  http.StatusForbidden,
		"router.lan.attacker.com":  http.StatusForbidden,
		"192.168.1.1.attacker.com": http.StatusForbidden,
	}

	handler := newHandler(security)

	for host, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		r.Host = host

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != want {
			t.Errorf("%s: code = %d, want %d", host, w.Code, want)
		}
	}

	// Updater в docker проверяет новую версию по имени контейнера: /api/health доступен по любому адресу,
	// остальные методы — нет, в том числе через .. и %2F.
	health := map[string]int{
		"/api/health":                 http.StatusOK,
		"/api/health/../config":       http.StatusForbidden,
		"/api/health/..%2Fconfig":     http.StatusForbidden,
		"/api/health%2F..%2Fsettings": http.StatusForbidden,
	}

	for target, want := range health {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.Host = "sing-box-configurer:8080"

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != want {
			t.Errorf("%s by container name: code = %d, want %d", target, w.Code, want)
		}
	}

	// Проверка выключена — любой адрес.
	security.CheckHost = false

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "attacker.example.net"

	w := httptest.NewRecorder()
	newHandler(security).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("check disabled: code = %d", w.Code)
	}
}

// TestNormalize проверяет приведение и проверку доменов.
func TestNormalize(t *testing.T) {
	result, err := settings.Security{
		CheckHost:    true,
		AllowedHosts: []string{" Router.LAN:8080 ", "router.lan", "*.Example.com", ""},
	}.Normalize()
	if err != nil {
		t.Fatal(err)
	}

	if len(result.AllowedHosts) != 2 || result.AllowedHosts[0] != "router.lan" || result.AllowedHosts[1] != "*.example.com" {
		t.Errorf("hosts = %v", result.AllowedHosts)
	}

	for _, bad := range []string{"http://router.lan", "a b", "*", "router.*.lan", "<script>"} {
		if _, err = (settings.Security{CheckHost: true, AllowedHosts: []string{bad}}).Normalize(); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
