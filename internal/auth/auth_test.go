package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// newManager создает менеджер с пустым app.json во временной папке.
func newManager(t *testing.T) (*Manager, *appdata.File) {
	t.Helper()

	appData := appdata.NewFile(filepath.Join(t.TempDir(), "app.json"))

	manager, err := NewManager(appData)
	if err != nil {
		t.Fatal(err)
	}

	return manager, appData
}

// TestPassword проверяет хэширование пароля.
func TestPassword(t *testing.T) {
	hash, err := HashPassword("secret-password")
	if err != nil {
		t.Fatal(err)
	}

	if !VerifyPassword("secret-password", hash) {
		t.Error("valid password must be accepted")
	}

	if VerifyPassword("secret-passwore", hash) || VerifyPassword("secret-password", "") || VerifyPassword("x", "plain$1$2$3") {
		t.Error("invalid password or hash must be rejected")
	}

	other, _ := HashPassword("secret-password")

	if other == hash {
		t.Error("hashes of the same password must use different salts")
	}
}

// TestUpdateAndLogin проверяет включение входа, вход, смену пароля и выключение.
func TestUpdateAndLogin(t *testing.T) {
	manager, appData := newManager(t)

	if manager.Enabled() {
		t.Fatal("auth must be disabled on a new installation")
	}

	if err := manager.Update(Update{Enabled: true, Username: "admin", Password: "", CurrentPassword: ""}); err == nil {
		t.Error("enabling without password must fail")
	}

	if err := manager.Update(Update{Enabled: true, Username: "admin", Password: "short", CurrentPassword: ""}); err == nil {
		t.Error("short password must be rejected")
	}

	if err := manager.Update(Update{Enabled: true, Username: " admin ", Password: "password-1", CurrentPassword: ""}); err != nil {
		t.Fatal(err)
	}

	if status := manager.Status(); !status.Enabled || status.Username != "admin" {
		t.Errorf("status = %+v", status)
	}

	if _, err := manager.Login("admin", "wrong-password", "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}

	token, err := manager.Login("admin", "password-1", "10.0.0.1")
	if err != nil || !manager.Valid(token) {
		t.Fatalf("login failed: %v", err)
	}

	// Учетные данные сохраняются в app.json и загружаются заново.
	reloaded, err := NewManager(appData)
	if err != nil || !reloaded.Valid(token) {
		t.Fatalf("reloaded manager must accept the session: %v", err)
	}

	if err = manager.Update(Update{Enabled: true, Username: "root", Password: "", CurrentPassword: "bad"}); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Errorf("change without current password: %v", err)
	}

	// Смена логина без нового пароля оставляет пароль и делает прежние сессии недействительными.
	if err = manager.Update(Update{Enabled: true, Username: "root", Password: "", CurrentPassword: "password-1"}); err != nil {
		t.Fatal(err)
	}

	if manager.Valid(token) {
		t.Error("old session must be invalid after credentials change")
	}

	if _, err = manager.Login("root", "password-1", "10.0.0.1"); err != nil {
		t.Errorf("login with new username: %v", err)
	}

	if err = manager.Update(Update{Enabled: false, Username: "", Password: "", CurrentPassword: "password-1"}); err != nil {
		t.Fatal(err)
	}

	if manager.Enabled() || manager.Status().Username != "" {
		t.Error("auth must be disabled and credentials removed")
	}
}

// TestSetAndReset проверяет команды auth set и auth reset.
func TestSetAndReset(t *testing.T) {
	manager, appData := newManager(t)

	if err := manager.Set("admin", "password-1"); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := NewManager(appData)

	if _, err := reloaded.Login("admin", "password-1", "10.0.0.1"); err != nil {
		t.Errorf("login after set: %v", err)
	}

	if err := reloaded.Reset(); err != nil {
		t.Fatal(err)
	}

	reloaded, _ = NewManager(appData)

	if reloaded.Enabled() {
		t.Error("auth must be disabled after reset")
	}
}

// TestValidToken проверяет отказ для поддельных и просроченных токенов.
func TestValidToken(t *testing.T) {
	manager, _ := newManager(t)

	if err := manager.Set("admin", "password-1"); err != nil {
		t.Fatal(err)
	}

	token := manager.NewSession()

	cases := map[string]string{
		"empty":     "",
		"no dot":    "abc",
		"tampered":  token + "x",
		"expired":   manager.newToken(manager.data, time.Now().Add(-time.Minute)),
		"other key": "YWRtaW58OTk5OTk5OTk5OQ.c2lnbmF0dXJl",
	}

	for name, value := range cases {
		if manager.Valid(value) {
			t.Errorf("%s token must be invalid", name)
		}
	}

	if !manager.Valid(token) {
		t.Error("fresh token must be valid")
	}
}

// TestLimiter проверяет блокировку после серии неудачных попыток.
func TestLimiter(t *testing.T) {
	manager, _ := newManager(t)

	if err := manager.Set("admin", "password-1"); err != nil {
		t.Fatal(err)
	}

	for range maxFailures {
		_, _ = manager.Login("admin", "wrong", "10.0.0.2")
	}

	var rateErr *RateLimitError

	if _, err := manager.Login("admin", "password-1", "10.0.0.2"); !errors.As(err, &rateErr) {
		t.Errorf("expected rate limit, got %v", err)
	}

	if _, err := manager.Login("admin", "password-1", "10.0.0.3"); err != nil {
		t.Errorf("other clients must not be limited: %v", err)
	}

	// Окно попыток прошло.
	manager.limiter.now = func() time.Time {
		return time.Now().Add(failureWindow + time.Second)
	}

	if _, err := manager.Login("admin", "password-1", "10.0.0.2"); err != nil {
		t.Errorf("limit must expire: %v", err)
	}
}

// TestMiddleware проверяет, какие запросы требуют входа.
func TestMiddleware(t *testing.T) {
	manager, _ := newManager(t)

	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := func(path string, token string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)

		if token != "" {
			r.AddCookie(&http.Cookie{
				Name:  CookieName,
				Value: token,
			})
		}

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		return w.Code
	}

	if code := request("/api/config", ""); code != http.StatusOK {
		t.Errorf("auth disabled: /api/config = %d", code)
	}

	if err := manager.Set("admin", "password-1"); err != nil {
		t.Fatal(err)
	}

	cases := map[string]int{
		"/":                          http.StatusOK,
		"/assets/index.js":           http.StatusOK,
		"/api/health":                http.StatusOK,
		"/api/auth/status":           http.StatusOK,
		"/api/ruleset/domain":        http.StatusOK,
		"/api/config":                http.StatusUnauthorized,
		"/api/auth/settings":         http.StatusUnauthorized,
		"/api/ruleset/../config":     http.StatusUnauthorized,
		"/api/update/start":          http.StatusUnauthorized,
		"/api/health/../settings/x":  http.StatusUnauthorized,
		"/api/ruleset/bypass?x=1":    http.StatusOK,
		"/api/clash/proxies?token=1": http.StatusUnauthorized,
	}

	for path, want := range cases {
		if code := request(path, ""); code != want {
			t.Errorf("%s = %d, want %d", path, code, want)
		}
	}

	if code := request("/api/config", manager.NewSession()); code != http.StatusOK {
		t.Errorf("with session: /api/config = %d", code)
	}
}
