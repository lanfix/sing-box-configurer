package render

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
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
		Subscriptions: []Subscription{
			{
				Name: "Happ",
				Outbounds: []map[string]any{
					{"type": "vless", "tag": "sub-1"},
					{"type": "vless", "tag": "vless-1"},
				},
			},
		},
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

	// Зарезервированный тег auto и повторяющийся vless-1 из подписки пропускаются.
	if len(result.Warnings) != 3 {
		t.Errorf("warnings = %v, want 3 (auto, duplicate, missing outbound)", result.Warnings)
	}

	outbounds := field(t, result.Config, "outbounds").([]any)

	auto := byTag(outbounds, "auto")
	if got := toJSON(t, auto["outbounds"]); got != `["vless-1","wg-1","sub-1"]` {
		t.Errorf("auto members = %s", got)
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

func TestRenderEmpty(t *testing.T) {
	result, err := Render(Input{RuleSetBaseURL: "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}

	outbounds := field(t, result.Config, "outbounds").([]any)

	// Без прокси urltest auto получает direct: пустой urltest sing-box не примет.
	if got := toJSON(t, byTag(outbounds, "auto")["outbounds"]); got != `["direct"]` {
		t.Errorf("auto members = %s", got)
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
	again, _ := Render(Input{RuleSetBaseURL: "http://127.0.0.1:8080"})

	if toJSON(t, result.Config) != toJSON(t, again.Config) {
		t.Error("render must be deterministic")
	}
}
