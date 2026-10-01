package happ

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

func TestUpdateInterval(t *testing.T) {
	cases := []struct {
		name  string
		hours int
		info  bool
		want  time.Duration
	}{
		{name: "no info", hours: 0, info: false, want: defaultUpdateInterval},
		{name: "not set", hours: 0, info: true, want: defaultUpdateInterval},
		{name: "negative", hours: -5, info: true, want: defaultUpdateInterval},
		{name: "hour", hours: 1, info: true, want: time.Hour},
		{name: "overflow", hours: 1 << 50, info: true, want: maxUpdateInterval},
	}

	for _, tc := range cases {
		profile := Profile{}

		if tc.info {
			profile.Info = &ProfileInfo{UpdateInterval: tc.hours}
		}

		if got := updateInterval(profile); got != tc.want {
			t.Errorf("%s: interval = %v, want %v", tc.name, got, tc.want)
		}

		if got := updateInterval(profile); got < minUpdateInterval {
			t.Errorf("%s: interval %v is less than minimum", tc.name, got)
		}
	}
}

func TestSafeURL(t *testing.T) {
	cases := map[string]string{
		"https://support.example/chat": "https://support.example/chat",
		" http://example.com ":         "http://example.com",
		"javascript:alert(1)":          "",
		"JavaScript:alert(1)":          "",
		"data:text/html,<script>":      "",
		"//evil.example":               "",
		"":                             "",
	}

	for value, want := range cases {
		if got := SafeURL(value); got != want {
			t.Errorf("SafeURL(%q) = %q, want %q", value, got, want)
		}
	}
}

// TestNotifyChangedServers проверяет, что обработчик получает прежнее состояние только профилей с изменившимися серверами.
func TestNotifyChangedServers(t *testing.T) {
	store, err := NewStore(appdata.NewFile(filepath.Join(t.TempDir(), "app.json")))
	if err != nil {
		t.Fatal(err)
	}

	server := func(tag string) Server {
		return Server{Tag: tag, Outbound: map[string]any{"type": "vless", "tag": tag}}
	}

	changed := Profile{ID: "changed", Name: "changed", Servers: []Server{server("a")}}
	same := Profile{ID: "same", Name: "same", Servers: []Server{server("b")}}

	for _, profile := range []Profile{changed, same} {
		if err = store.Put(profile); err != nil {
			t.Fatal(err)
		}
	}

	updated := changed
	updated.Servers = []Server{server("c")}

	if err = store.Put(updated); err != nil {
		t.Fatal(err)
	}

	var received []outbound.Subscription

	manager := NewManager(store, NewClient())
	manager.SetUpdateListener(func(_ context.Context, previous []outbound.Subscription) (string, error) {
		received = previous

		return "applied", nil
	})

	if message := manager.notifyLocked(context.Background(), []Profile{changed, same}); message != "applied" {
		t.Errorf("message = %q", message)
	}

	if len(received) != 1 || received[0].ProfileID != "changed" || received[0].Outbounds[0]["tag"] != "a" {
		t.Errorf("previous = %+v", received)
	}

	received = nil

	if message := manager.notifyLocked(context.Background(), []Profile{same}); message != "" || received != nil {
		t.Errorf("listener must not be called without changes: %q %+v", message, received)
	}
}
