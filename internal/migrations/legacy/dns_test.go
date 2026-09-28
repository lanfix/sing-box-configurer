package legacy

import (
	"encoding/json"
	"reflect"
	"testing"
)

// manualDNSConfig — конфиг с DNS-правилами групп и пользовательскими правилами.
const manualDNSConfig = `{
	"dns": {
		"final": "yandex",
		"rules": [
			{"domain": ["ha.home.lab"], "server": "static-hosts"},
			{"action": "predefined", "query_type": ["HTTPS"], "rcode": "NOERROR"},
			{"domain": ["claude.ai"], "server": "cloudflare"},
			{"rule_set": "configurer-default", "server": "cloudflare"},
			{"rule_set": "configurer-claude", "server": "cloudflare"},
			{"action": "evaluate", "rule_set": "configurer-block@ip", "server": "google", "tag": "configurer-block@dns"},
			{"action": "route", "preferred_by": ["local"], "server": "local"}
		]
	}
}`

// dnsRules возвращает dns.rules конфига в виде JSON-строк для наглядного сравнения.
func dnsRules(t *testing.T, config map[string]any) []string {
	t.Helper()

	dns, _ := config["dns"].(map[string]any)
	rules, _ := dns["rules"].([]any)
	result := make([]string, 0, len(rules))

	for _, rule := range rules {
		raw, err := json.Marshal(rule)
		if err != nil {
			t.Fatal(err)
		}

		result = append(result, string(raw))
	}

	return result
}

func TestGetGroupDNSServers(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)

	want := map[string]string{
		"default": "cloudflare",
		"claude":  "cloudflare",
	}

	if got := GetGroupDNSServers(config); !reflect.DeepEqual(got, want) {
		t.Errorf("GetGroupDNSServers = %v, want %v", got, want)
	}
}

func TestParseSystemDNSRule(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)
	rules := config["dns"].(map[string]any)["rules"].([]any)

	type result struct {
		group  string
		isHead bool
		ok     bool
	}

	want := []result{
		{group: "", isHead: false, ok: false},
		{group: "", isHead: false, ok: false},
		{group: "", isHead: false, ok: false},
		{group: "default", isHead: true, ok: true},
		{group: "claude", isHead: true, ok: true},
		{group: "block", isHead: false, ok: true},
		{group: "", isHead: false, ok: false},
	}

	for i, item := range rules {
		group, isHead, ok := ParseSystemDNSRule(item.(map[string]any))

		if got := (result{group: group, isHead: isHead, ok: ok}); got != want[i] {
			t.Errorf("rule %d: got %+v, want %+v", i, got, want[i])
		}
	}

	if !IsHTTPSFilterRule(rules[1].(map[string]any)) {
		t.Error("rule 1 should be https filter")
	}
}
