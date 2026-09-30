package rules

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// newCachedManager создает менеджер с app.json и кэшем источников во временной папке.
func newCachedManager(t *testing.T, dir string, opts Options) *Manager {
	t.Helper()

	opts.CacheDir = filepath.Join(dir, "url-sources")

	manager, err := NewManager(appdata.NewFile(filepath.Join(dir, "app.json")), opts)
	if err != nil {
		t.Fatal(err)
	}

	if err = manager.Load(); err != nil {
		t.Fatal(err)
	}

	return manager
}

// TestSourceCache проверяет, что после перезапуска список источника берется из кэша, пока новая загрузка
// не удалась, а удаленный источник удаляет и кэш.
func TestSourceCache(t *testing.T) {
	online := true

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !online {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		_, _ = fmt.Fprintln(w, "149.154.160.0/20\ntelegram.org")
	}))
	defer server.Close()

	dir := t.TempDir()
	manager := newCachedManager(t, dir, Options{})

	if err := manager.AddURLSource(URLSource{ID: "src", URL: server.URL, Group: DefaultGroupName, Interval: 60}); err != nil {
		t.Fatal(err)
	}

	manager.mu.Lock()
	manager.data.URLSources[0].Applied = true
	manager.mu.Unlock()

	if err := manager.fetchRulesFromSource("src"); err != nil {
		t.Fatal(err)
	}

	// «Перезапуск»: источник недоступен, набор все равно готов сразу — из кэша.
	online = false
	restarted := newCachedManager(t, dir, Options{})

	ruleSet := mustRuleSet(t)(restarted.GetRuleSetByGroup(DefaultGroupName, RuleSetKindIP))

	if got := ruleSetValues(t, ruleSet, "ip_cidr"); !slices.Equal(got, []string{"149.154.160.0/20"}) {
		t.Errorf("cached ip_cidr = %v", got)
	}

	if err := restarted.fetchRulesFromSource("src"); err == nil {
		t.Fatal("fetch must fail while the source is offline")
	}

	ruleSet = mustRuleSet(t)(restarted.GetRuleSetByGroup(DefaultGroupName, RuleSetKindDomain))

	if got := ruleSetValues(t, ruleSet, "domain_suffix"); !slices.Equal(got, []string{"telegram.org"}) {
		t.Errorf("failed fetch must keep cached set, domain_suffix = %v", got)
	}

	if err := restarted.DeleteURLSource("src"); err != nil {
		t.Fatal(err)
	}

	if err := restarted.ApplyURLSources(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "url-sources", "src.json")); !os.IsNotExist(err) {
		t.Errorf("cache of deleted source must be removed: %v", err)
	}
}

// TestFailedSourceDoesNotBlockGroup проверяет, что источник без кэша, который не загрузился, не держит группу.
func TestFailedSourceDoesNotBlockGroup(t *testing.T) {
	manager := newKindTestManager()

	delete(manager.urlRules, "src")

	if _, err := manager.GetRuleSetByGroup("default", RuleSetKindAll); err == nil {
		t.Fatal("group must wait for the first fetch attempt")
	}

	manager.attempted["src"] = true

	ruleSet := mustRuleSet(t)(manager.GetRuleSetByGroup("default", RuleSetKindAll))

	if got := ruleSetValues(t, ruleSet, "domain"); !slices.Equal(got, []string{"claude.ai"}) {
		t.Errorf("domain = %v, want manual rules only", got)
	}
}

// TestDetourProxy проверяет, что источник с detour загружается через прокси этого outbound-а.
func TestDetourProxy(t *testing.T) {
	var proxied []string

	// Прокси-сервер: запоминает, какой outbound (логин) запросил загрузку, и отвечает списком.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()

		if header := r.Header.Get("Proxy-Authorization"); header != "" {
			request := &http.Request{Header: http.Header{"Authorization": {header}}}
			user, _, _ = request.BasicAuth()
		}

		proxied = append(proxied, user)

		_, _ = fmt.Fprintln(w, "example.org")
	}))
	defer proxy.Close()

	manager := newCachedManager(t, t.TempDir(), Options{
		SourceListsProxyURL: "",
		CacheDir:            "",
		DetourProxy: func(detour string) (*url.URL, error) {
			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				return nil, err
			}

			proxyURL.User = url.UserPassword(detour, "secret")

			return proxyURL, nil
		},
	})

	ruleSet, err := manager.GatherRuleSetFromURL("http://lists.invalid/list.txt", "vpn-auto")
	if err != nil {
		t.Fatal(err)
	}

	if ruleSet.Total() != 1 || !slices.Equal(proxied, []string{"vpn-auto"}) {
		t.Errorf("ruleSet total = %d, proxied = %v", ruleSet.Total(), proxied)
	}

	if err = manager.AddURLSource(URLSource{ID: "a", URL: "http://a.invalid", Group: DefaultGroupName, Interval: 60, Detour: " vpn-auto "}); err != nil {
		t.Fatal(err)
	}

	if err = manager.AddURLSource(URLSource{ID: "b", URL: "http://b.invalid", Group: DefaultGroupName, Interval: 60, Detour: "vpn-auto"}); err != nil {
		t.Fatal(err)
	}

	if got := manager.Detours(); !slices.Equal(got, []string{"vpn-auto"}) {
		t.Errorf("Detours = %v", got)
	}
}
