package speedtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// newProxy запускает HTTP-прокси, который вместо пересылки сам отвечает на запросы: blocked.test недоступен,
// остальные хосты отдают поток данных.
func newProxy(t *testing.T, handler http.HandlerFunc) *url.URL {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	proxyURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	return proxyURL
}

// streamData отдает данные блоками, пока клиент не отключится или не будет отдано limit байт.
func streamData(w http.ResponseWriter, r *http.Request, limit int) {
	chunk := make([]byte, 64<<10)

	w.WriteHeader(http.StatusOK)

	for sent := 0; sent < limit && r.Context().Err() == nil; sent += len(chunk) {
		if _, err := w.Write(chunk); err != nil {
			return
		}
	}
}

// testSettings — два сервера: первый недоступен через прокси, второй отвечает.
func testSettings() settings.SpeedTest {
	return settings.SpeedTest{
		Servers: []settings.SpeedTestServer{
			{Name: "Blocked", URL: "http://blocked.test/file"},
			{Name: "Working", URL: "http://ok.test/file"},
		},
		Duration: 1,
		Streams:  2,
	}
}

// TestRunFallsBackToNextServer проверяет переход к следующему серверу, если первый недоступен через outbound.
func TestRunFallsBackToNextServer(t *testing.T) {
	var (
		mu    sync.Mutex
		users []string
	)

	proxyURL := newProxy(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		users = append(users, r.Header.Get("Proxy-Authorization"))
		mu.Unlock()

		if r.URL.Host == "blocked.test" {
			http.Error(w, "blocked", http.StatusBadGateway)

			return
		}

		streamData(w, r, 8<<20)
	})

	service := NewService(func(tag string) (*url.URL, error) {
		withUser := *proxyURL
		withUser.User = url.UserPassword(tag, "secret")

		return &withUser, nil
	}, testSettings)

	result, err := service.Run(context.Background(), "vless-1", "")
	if err != nil {
		t.Fatal(err)
	}

	if result.Error != "" || result.Server != "Working" || result.Bytes == 0 || result.Download <= 0 {
		t.Fatalf("result = %+v", result)
	}

	if len(result.Attempts) != 1 || result.Attempts[0].Server != "Blocked" {
		t.Errorf("attempts = %+v", result.Attempts)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(users) == 0 || users[0] == "" {
		t.Error("requests must carry proxy credentials of the outbound")
	}

	running, results := service.Status()
	if running != "" || results["vless-1"].Server != "Working" {
		t.Errorf("status = %q, %+v", running, results)
	}
}

// TestRunStopsOnProxyAuth проверяет, что отказ служебного inbound-а (outbound-а нет в рабочем конфиге)
// останавливает перебор серверов.
func TestRunStopsOnProxyAuth(t *testing.T) {
	var requests atomic.Int32

	proxyURL := newProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusProxyAuthRequired)
	})

	service := NewService(func(string) (*url.URL, error) {
		return proxyURL, nil
	}, testSettings)

	result, err := service.Run(context.Background(), "new-outbound", "")
	if err != nil {
		t.Fatal(err)
	}

	if requests.Load() != 1 || !strings.Contains(result.Error, "примените конфиг") {
		t.Errorf("requests = %d, result = %+v", requests.Load(), result)
	}
}

// TestRunOutboundFailure проверяет, что outbound, через который sing-box закрывает все соединения, отличается
// от недоступных тестовых серверов.
func TestRunOutboundFailure(t *testing.T) {
	proxyURL := newProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})

	service := NewService(func(string) (*url.URL, error) {
		return proxyURL, nil
	}, testSettings)

	result, err := service.Run(context.Background(), "broken", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Attempts) != 2 || !strings.Contains(result.Error, "Outbound не устанавливает соединения") {
		t.Errorf("result = %+v", result)
	}
}

// TestRunUnknownServer проверяет, что замер идет только с серверов из настроек.
func TestRunUnknownServer(t *testing.T) {
	service := NewService(func(string) (*url.URL, error) {
		return &url.URL{}, nil
	}, testSettings)

	if _, err := service.Run(context.Background(), "direct", "http://other.test/file"); err != ErrUnknownServer {
		t.Errorf("err = %v, want ErrUnknownServer", err)
	}
}
