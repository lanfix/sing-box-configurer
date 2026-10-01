package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProtect проверяет, что изменяющие запросы с чужих сайтов отклоняются, а свои и не из браузера — нет.
func TestProtect(t *testing.T) {
	handler := Protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{name: "cross-site post", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden},
		{name: "same-site post", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "same-site"}, want: http.StatusForbidden},
		{name: "foreign origin post", method: http.MethodPost, headers: map[string]string{"Origin": "http://evil.example"}, want: http.StatusForbidden},
		{name: "same-origin post", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, want: http.StatusOK},
		{name: "own origin post", method: http.MethodPost, headers: map[string]string{"Origin": "http://router.lan:8080"}, want: http.StatusOK},
		{name: "non-browser post", method: http.MethodPost, headers: map[string]string{}, want: http.StatusOK},
		{name: "cross-site get", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusOK},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://router.lan:8080/api/apply", nil)

		for key, value := range tc.headers {
			r.Header.Set(key, value)
		}

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != tc.want {
			t.Errorf("%s: code = %d, want %d", tc.name, w.Code, tc.want)
		}

		if w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s: X-Frame-Options is not set", tc.name)
		}
	}
}
