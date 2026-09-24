package singboxconfig

import (
	"encoding/json"
	"reflect"
	"testing"
)

// parseConfig разбирает JSON-конфиг так же, как это делает провайдер.
func parseConfig(t *testing.T, raw string) map[string]any {
	t.Helper()

	var config map[string]any

	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}

	return config
}

// countBypass считает системные записи исключений в конфиге.
func countBypass(config map[string]any) (ruleSets, rules int) {
	route, _ := config["route"].(map[string]any)

	for _, item := range route["rule_set"].([]any) {
		if tag, _ := item.(map[string]any)["tag"].(string); tag == BypassRuleSetTag {
			ruleSets++
		}
	}

	for _, item := range route["rules"].([]any) {
		if isBypassRule(item) {
			rules++
		}
	}

	return ruleSets, rules
}

const autoRedirectConfig = `{
	"dns": {"final": "yandex"},
	"inbounds": [
		{"type": "tun", "tag": "tun-in", "auto_route": true, "auto_redirect": true, "route_exclude_address_set": "geoip-ru"},
		{"type": "mixed", "tag": "mixed-in"}
	],
	"route": {
		"rule_set": [
			{"type": "local", "tag": "geoip-ru", "format": "source", "path": "/var/lib/sing-box/geoip-ru.json"},
			{"type": "remote", "tag": "configurer-default", "format": "source", "url": "http://127.0.0.1:8080/api/ruleset/group?group=default"}
		],
		"rules": [
			{"action": "sniff"},
			{"action": "hijack-dns", "protocol": "dns"},
			{"rule_set": "configurer-default", "outbound": "select-default"}
		]
	}
}`

func TestEnsureBypassAutoRedirect(t *testing.T) {
	config := parseConfig(t, autoRedirectConfig)

	// Повторный вызов не должен создавать дубликаты.
	for range 2 {
		if err := EnsureBypass(config); err != nil {
			t.Fatal(err)
		}
	}

	ruleSets, rules := countBypass(config)

	if ruleSets != 1 || rules != 1 {
		t.Fatalf("bypass rule-sets = %d, rules = %d, want 1 and 1", ruleSets, rules)
	}

	route := config["route"].(map[string]any)

	// Правило исключений должно стоять раньше sniff, иначе pre-match TCP до него не дойдет.
	if !isBypassRule(route["rules"].([]any)[0]) {
		t.Errorf("first rule = %v, want bypass rule", route["rules"].([]any)[0])
	}

	tun := config["inbounds"].([]any)[0].(map[string]any)

	if got, want := tun["route_exclude_address_set"], []any{"geoip-ru", BypassRuleSetTag}; !reflect.DeepEqual(got, want) {
		t.Errorf("route_exclude_address_set = %v, want %v", got, want)
	}

	mixed := config["inbounds"].([]any)[1].(map[string]any)

	if _, ok := mixed["route_exclude_address_set"]; ok {
		t.Error("mixed inbound must not get route_exclude_address_set")
	}

	if reverseMapping, _ := config["dns"].(map[string]any)["reverse_mapping"].(bool); !reverseMapping {
		t.Error("dns.reverse_mapping should be enabled")
	}
}

func TestEnsureBypassRemovesWithoutAutoRedirect(t *testing.T) {
	config := parseConfig(t, autoRedirectConfig)

	if err := EnsureBypass(config); err != nil {
		t.Fatal(err)
	}

	tun := config["inbounds"].([]any)[0].(map[string]any)
	tun["auto_redirect"] = false

	if err := EnsureBypass(config); err != nil {
		t.Fatal(err)
	}

	ruleSets, rules := countBypass(config)

	if ruleSets != 0 || rules != 0 {
		t.Fatalf("bypass rule-sets = %d, rules = %d, want none", ruleSets, rules)
	}

	// Пользовательский rule-set в исключениях остается.
	if got, want := tun["route_exclude_address_set"], []any{"geoip-ru"}; !reflect.DeepEqual(got, want) {
		t.Errorf("route_exclude_address_set = %v, want %v", got, want)
	}
}

func TestSyncGroupsKeepsBypass(t *testing.T) {
	config := parseConfig(t, autoRedirectConfig)

	if err := EnsureBypass(config); err != nil {
		t.Fatal(err)
	}

	groups := []Group{
		{
			Name:            "default",
			Description:     "",
			DefaultOutbound: "direct",
		},
	}

	// syncGroups отбрасывает неизвестные системные rule-set-ы, EnsureBypass их восстанавливает.
	if err := syncGroups(config, groups, groups); err != nil {
		t.Fatal(err)
	}

	if err := EnsureBypass(config); err != nil {
		t.Fatal(err)
	}

	ruleSets, rules := countBypass(config)

	if ruleSets != 1 || rules != 1 {
		t.Fatalf("bypass rule-sets = %d, rules = %d, want 1 and 1", ruleSets, rules)
	}
}
