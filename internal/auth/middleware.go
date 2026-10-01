package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

var (
	// publicPaths — методы API, доступные без входа: health-check updater, вход и выход.
	publicPaths = []string{"/api/health", "/api/auth/status", "/api/auth/login", "/api/auth/logout"}

	// publicPrefixes — rule-set-ы групп забирает sing-box, у него нет сессии.
	publicPrefixes = []string{"/api/ruleset/"}
)

// Middleware пропускает к API только запросы с действующей сессией, если вход включен.
// Файлы интерфейса отдаются всегда: страница входа — часть интерфейса.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Путь очищается так же, как это сделает ServeMux: /api/ruleset/../config — это /api/config.
		// ServeMux сопоставляет экранированный путь, поэтому проверяются оба варианта: %2F внутри
		// сегмента (/api/x/..%2F..%2Fy) не должен уводить проверку на путь вне /api/.
		paths := []string{path.Clean("/" + r.URL.Path), path.Clean("/" + r.URL.EscapedPath())}

		if !slices.ContainsFunc(paths, isProtected) || m.Authenticated(r) {
			next.ServeHTTP(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "Требуется вход",
		})
	})
}

// SetSessionCookie устанавливает cookie сессии.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(SessionTTL),
		MaxAge:   int(SessionTTL.Seconds()),
		Secure:   secureRequest(r),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie удаляет cookie сессии.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secureRequest(r),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClientIP возвращает IP-адрес клиента для ограничения попыток входа. Заголовки прокси
// не учитываются: их может подделать сам клиент.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// isProtected проверяет, что путь — метод API, закрытый входом.
func isProtected(path string) bool {
	return strings.HasPrefix(path, "/api/") && !isPublic(path)
}

// isPublic проверяет, что метод API доступен без входа.
func isPublic(path string) bool {
	if slices.Contains(publicPaths, path) {
		return true
	}

	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// secureRequest проверяет, что запрос пришел по HTTPS (напрямую или через reverse proxy).
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
