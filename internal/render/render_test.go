package render

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// testInput — данные рендера с группами, DNS, ручными outbound-ами и подпиской.
func testInput() Input {
	return Input{
		Groups: []rules.Group{
			{Name: "default", DefaultOutbound: "auto", DNSServer: "cloudflare"},
			{Name: "claude", DefaultOutbound: "missing"},
		},
		DNS: dnsconfig.Data{
			Servers: []dnsconfig.Server{
				{Tag: "yandex", Type: dnsconfig.TypeUDP, Server: "77.88.8.1"},
				{Tag: "cloudflare", Type: dnsconfig.TypeHTTPS, Server: "1.1.1.1", TLSServerName: "cloudflare-dns.com"},
			},
			Settings: dnsconfig.Settings{
				Final:                 "yandex",
				Strategy:              "ipv4_only",
				DefaultDomainResolver: "yandex",
			},
			Rules: []map[string]any{
				{"domain": []any{"claude.ai"}, "server": "cloudflare"},
			},
		},
		DNSRecords: []dnsrecords.Record{
			{Domain: "ha.home.lab", Addresses: []string{"192.168.50.8"}},
		},
		Outbounds: []map[string]any{
			{"type": "vless", "tag": "vless-1", "server": "a.example"},
			{"type": "wireguard", "tag": "wg-1"},
			{"type": "selector", "tag": "manual-select", "outbounds": []any{"vless-1"}},
			{"type": "vless", "tag": "auto"},
		},
		Subscriptions: []outbound.Subscription{
			{
				Source:      outbound.SourceHapp,
				ProfileID:   "happ-1",
				ProfileName: "Happ",
				Outbounds: []map[string]any{
					{"type": "vless", "tag": "sub-1"},
					{"type": "vless", "tag": "vless-1"},
				},
			},
		},
		URLTests: outbound.DefaultURLTests(),
		Mixed: []inbounds.Mixed{
			{Tag: "mixed-proxy", Listen: "0.0.0.0", ListenPort: 1080},
		},
		Settings: settings.Settings{
			LogLevel: "info",
			ClashAPI: settings.ClashAPI{Secret: "secret", AllowOrigins: []string{"*"}},
		},
		RuleSetBaseURL: "http://127.0.0.1:8080",
	}
}

// field возвращает вложенное значение конфига по пути ключей.
func field(t *testing.T, config map[string]any, keys ...string) any {
	t.Helper()

	var value any = config

	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%v: not an object at %s", keys, key)
		}

		value = object[key]
	}

	return value
}

// toJSON сериализует значение для сравнения.
func toJSON(t *testing.T, value any) string {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// byTag находит объект с тегом tag в списке.
func byTag(list []any, tag string) map[string]any {
	for _, item := range list {
		if object, ok := item.(map[string]any); ok && object["tag"] == tag {
			return object
		}
	}

	return nil
}

func TestRenderOutbounds(t *testing.T) {
	result, err := Render(testInput())
	if err != nil {
		t.Fatal(err)
	}

	// Тег urltest-а auto и повторяющийся vless-1 из подписки пропускаются.
	if len(result.Warnings) != 3 {
		t.Errorf("warnings = %v, want 3 (auto, duplicate, missing outbound)", result.Warnings)
	}

	outbounds := field(t, result.Config, "outbounds").([]any)

	auto := byTag(outbounds, "auto")
	if got := toJSON(t, auto["outbounds"]); got != `["vless-1","wg-1","sub-1"]` {
		t.Errorf("auto members = %s", got)
	}

	if auto["url"] != outbound.DefaultTestURL || auto["interval"] != outbound.DefaultInterval || auto["tolerance"] != outbound.DefaultTolerance {
		t.Errorf("auto params = %v", auto)
	}

	selector := byTag(outbounds, "select-default")
	if got := toJSON(t, selector["outbounds"]); got != `["auto","vless-1","wg-1","manual-select","sub-1","direct","block"]` {
		t.Errorf("selector members = %s", got)
	}

	if selector["default"] != "auto" {
		t.Errorf("default = %v", selector["default"])
	}

	// Outbound группы не найден — трафик блокируется.
	if byTag(outbounds, "select-claude")["default"] != "block" {
		t.Error("missing default outbound must fall back to block")
	}

	endpoints := field(t, result.Config, "endpoints").([]any)
	if len(endpoints) != 1 || byTag(endpoints, "wg-1") == nil {
		t.Errorf("endpoints = %v", endpoints)
	}

	if byTag(outbounds, "wg-1") != nil {
		t.Error("wireguard must be rendered as endpoint")
	}
}

func TestRenderRoute(t *testing.T) {
	result, err := Render(testInput())
	if err != nil {
		t.Fatal(err)
	}

	routeRules := field(t, result.Config, "route", "rules").([]any)

	want := []string{
		`{"action":"bypass","rule_set":"configurer-bypass"}`,
		`{"action":"sniff","timeout":"500ms"}`,
		`{"action":"hijack-dns","port":53,"protocol":"dns"}`,
		`{"action":"resolve","inbound":"mixed-proxy"}`,
		`{"ip_is_private":true,"outbound":"direct"}`,
		`{"action":"reject","rule_set":["configurer-block","configurer-block@ip"]}`,
		`{"outbound":"select-default","rule_set":["configurer-default","configurer-default@ip"]}`,
		`{"outbound":"select-claude","rule_set":["configurer-claude","configurer-claude@ip"]}`,
	}

	got := make([]string, 0, len(routeRules))

	for _, rule := range routeRules {
		got = append(got, toJSON(t, rule))
	}

	if !slices.Equal(got, want) {
		t.Errorf("route rules:\n got %v\nwant %v", got, want)
	}

	ruleSets := field(t, result.Config, "route", "rule_set").([]any)

	if len(ruleSets) != 7 {
		t.Errorf("rule sets = %d, want 7", len(ruleSets))
	}

	if url := byTag(ruleSets, "configurer-default@ip")["url"]; url != "http://127.0.0.1:8080/api/ruleset/ip?group=default" {
		t.Errorf("rule set url = %v", url)
	}

	if resolver := field(t, result.Config, "route", "default_domain_resolver"); resolver != "yandex" {
		t.Errorf("default_domain_resolver = %v", resolver)
	}

	tun := field(t, result.Config, "inbounds").([]any)[0].(map[string]any)

	if got := toJSON(t, tun["route_exclude_address_set"]); got != `["configurer-bypass"]` {
		t.Errorf("tun route_exclude_address_set = %s", got)
	}
}

func TestRenderDNS(t *testing.T) {
	result, err := Render(testInput())
	if err != nil {
		t.Fatal(err)
	}

	dnsRules := field(t, result.Config, "dns", "rules").([]any)

	want := []string{
		`{"domain":["ha.home.lab"],"server":"configurer-hosts"}`,
		`{"action":"predefined","query_type":["HTTPS"],"rcode":"NOERROR"}`,
		`{"rule_set":"configurer-default","server":"cloudflare"}`,
		`{"domain":["claude.ai"],"server":"cloudflare"}`,
	}

	got := make([]string, 0, len(dnsRules))

	for _, rule := range dnsRules {
		got = append(got, toJSON(t, rule))
	}

	if !slices.Equal(got, want) {
		t.Errorf("dns rules:\n got %v\nwant %v", got, want)
	}

	servers := field(t, result.Config, "dns", "servers").([]any)

	if len(servers) != 3 || byTag(servers, "configurer-hosts") == nil {
		t.Errorf("dns servers = %v", servers)
	}

	if field(t, result.Config, "dns", "final") != "yandex" || field(t, result.Config, "dns", "reverse_mapping") != true {
		t.Error("dns settings must be applied")
	}

	if field(t, result.Config, "experimental", "clash_api", "secret") != "secret" {
		t.Error("clash api secret must be rendered")
	}

	if field(t, result.Config, "log", "level") != "info" {
		t.Error("log level must be rendered")
	}
}

func TestRenderURLTests(t *testing.T) {
	in := testInput()
	in.Groups = []rules.Group{
		{Name: "happ", DefaultOutbound: "happ-fast"},
	}
	in.URLTests = []outbound.URLTest{
		{
			Tag:           "happ-fast",
			Sources:       []outbound.URLTestSource{{Kind: outbound.SourceHapp, ProfileID: "happ-1"}},
			ExcludeRegexp: "^sub-2$",
			ExcludeTags:   []string{"sub-3"},
			Tags:          []string{"vless-1", "gone"},
		},
		{
			Tag:     "manual-only",
			Sources: []outbound.URLTestSource{{Kind: outbound.SourceManual}},
			// selector и urltest по источникам не подбираются.
			IncludeRegexp: "select|wg",
		},
		{
			Tag:     "empty",
			Sources: []outbound.URLTestSource{{Kind: outbound.SourceAmnezia}},
		},
	}
	in.Subscriptions[0].Outbounds = []map[string]any{
		{"type": "vless", "tag": "sub-1"},
		{"type": "vless", "tag": "sub-2"},
		{"type": "vless", "tag": "sub-3"},
		{"type": "urltest", "tag": "sub-balancer", "outbounds": []any{"sub-1"}},
	}

	result, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}

	outbounds := field(t, result.Config, "outbounds").([]any)

	if got := toJSON(t, byTag(outbounds, "happ-fast")["outbounds"]); got != `["sub-1","vless-1"]` {
		t.Errorf("happ-fast members = %s", got)
	}

	if got := toJSON(t, byTag(outbounds, "manual-only")["outbounds"]); got != `["wg-1"]` {
		t.Errorf("manual-only members = %s", got)
	}

	// Пустой urltest не выводится и не попадает в selector-ы.
	if byTag(outbounds, "empty") != nil {
		t.Error("empty urltest must be skipped")
	}

	selector := byTag(outbounds, "select-happ")

	if selector["default"] != "happ-fast" {
		t.Errorf("default = %v", selector["default"])
	}

	if got := toJSON(t, selector["outbounds"]); got != `["happ-fast","manual-only","vless-1","wg-1","manual-select","auto","sub-1","sub-2","sub-3","sub-balancer","direct","block"]` {
		t.Errorf("selector members = %s", got)
	}

	// Не найден явно указанный outbound gone, пустой urltest пропущен.
	for _, want := range []string{"urltest happ-fast: outbound gone не найден", "urltest empty пропущен — в нем нет outbound-ов"} {
		if !slices.Contains(result.Warnings, want) {
			t.Errorf("warnings = %v, want %q", result.Warnings, want)
		}
	}
}

func TestRenderEmpty(t *testing.T) {
	in := Input{
		Groups: []rules.Group{
			{Name: "default", DefaultOutbound: "auto"},
		},
		URLTests:       outbound.DefaultURLTests(),
		RuleSetBaseURL: "http://127.0.0.1:8080",
	}

	result, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}

	outbounds := field(t, result.Config, "outbounds").([]any)

	// Без прокси urltest auto не выводится (пустой urltest sing-box не примет), и группа получает block.
	if byTag(outbounds, "auto") != nil {
		t.Error("empty auto must be skipped")
	}

	if byTag(outbounds, "select-default")["default"] != "block" {
		t.Error("group with empty urltest must fall back to block")
	}

	if _, ok := result.Config["endpoints"]; ok {
		t.Error("endpoints must be omitted when empty")
	}

	// Без mixed-inbound-ов правило resolve не нужно.
	for _, rule := range field(t, result.Config, "route", "rules").([]any) {
		if rule.(map[string]any)["action"] == "resolve" {
			t.Error("resolve rule must be omitted without mixed inbounds")
		}
	}

	// Рендер детерминирован.
	again, _ := Render(in)

	if toJSON(t, result.Config) != toJSON(t, again.Config) {
		t.Error("render must be deterministic")
	}
}

// TestRenderSourcesProxy проверяет служебный inbound для загрузки URL-источников через outbound-ы.
func TestRenderSourcesProxy(t *testing.T) {
	in := testInput()

	// Без источников с detour inbound-а нет.
	result, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}

	if byTag(field(t, result.Config, "inbounds").([]any), SourcesProxyTag) != nil {
		t.Fatal("sources proxy inbound must not be rendered without detours")
	}

	in.Settings.SourcesProxy.Password = "pass"
	in.SourcesProxy = SourcesProxy{
		Listen:  "127.0.0.1",
		Port:    9091,
		Detours: []string{"vless-1", "select-default", "missing"},
	}

	result, err = Render(in)
	if err != nil {
		t.Fatal(err)
	}

	inbound := byTag(field(t, result.Config, "inbounds").([]any), SourcesProxyTag)
	if inbound == nil {
		t.Fatal("sources proxy inbound must be rendered")
	}

	wantUsers := `[{"password":"pass","username":"vless-1"},{"password":"pass","username":"select-default"}]`

	if got := toJSON(t, inbound["users"]); got != wantUsers || inbound["listen_port"] != 9091 {
		t.Errorf("inbound = %s", toJSON(t, inbound))
	}

	routeRules := field(t, result.Config, "route", "rules").([]any)
	wantFirst := `{"auth_user":["vless-1"],"inbound":"configurer-sources","outbound":"vless-1"}`

	if got := toJSON(t, routeRules[0]); got != wantFirst {
		t.Errorf("first route rule = %s, want %s", got, wantFirst)
	}

	if !slices.ContainsFunc(result.Warnings, func(warning string) bool {
		return strings.Contains(warning, "outbound missing для загрузки не найден")
	}) {
		t.Errorf("missing detour must produce a warning: %v", result.Warnings)
	}
}
