package singboxconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// manualDNSConfig — конфиг с DNS-правилами, заданными вручную до управления DNS из конфигуратора.
const manualDNSConfig = `{
	"dns": {
		"final": "yandex",
		"rules": [
			{"domain": ["ha.home.lab"], "server": "static-hosts"},
			{"domain": ["claude.ai"], "server": "cloudflare"},
			{"rule_set": "configurer-default", "server": "cloudflare"},
			{"rule_set": "configurer-claude", "server": "cloudflare"},
			{"action": "route", "preferred_by": ["local"], "server": "local"}
		]
	},
	"outbounds": [
		{"type": "direct", "tag": "direct"},
		{"type": "block", "tag": "block"}
	],
	"route": {
		"final": "direct",
		"rule_set": [
			{"type": "remote", "tag": "configurer-default", "format": "source", "url": "http://127.0.0.1:8080/api/ruleset/group?group=default"},
			{"type": "remote", "tag": "configurer-claude", "format": "source", "url": "http://127.0.0.1:8080/api/ruleset/group?group=claude"}
		],
		"rules": [
			{"action": "sniff"},
			{"rule_set": "configurer-default", "outbound": "select-default"},
			{"rule_set": "configurer-claude", "outbound": "select-claude"}
		]
	}
}`

// testGroups — группы default и claude с DNS-сервером и block без него.
var testGroups = []Group{
	{
		Name:            "default",
		Description:     "",
		DefaultOutbound: "direct",
		DNSServer:       "cloudflare",
	},
	{
		Name:            "claude",
		Description:     "",
		DefaultOutbound: "direct",
		DNSServer:       "cloudflare",
	},
	{
		Name:            "block",
		Description:     "",
		DefaultOutbound: "block",
		DNSServer:       "",
	},
}

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

// roundTrip сериализует и разбирает конфиг, как это происходит при записи во временный файл.
func roundTrip(t *testing.T, config map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	return parseConfig(t, string(raw))
}

func TestSyncGroupsDNSRules(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)

	if err := syncGroups(config, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	want := []string{
		`{"action":"predefined","query_type":["HTTPS"],"rcode":"NOERROR"}`,
		`{"rule_set":"configurer-default","server":"cloudflare"}`,
		`{"rule_set":"configurer-claude","server":"cloudflare"}`,
		`{"domain":["ha.home.lab"],"server":"static-hosts"}`,
		`{"domain":["claude.ai"],"server":"cloudflare"}`,
		`{"action":"route","preferred_by":["local"],"server":"local"}`,
	}

	if got := dnsRules(t, config); !slices.Equal(got, want) {
		t.Errorf("dns rules:\n got %v\nwant %v", got, want)
	}

	// Повторная синхронизация ничего не меняет.
	again := roundTrip(t, config)

	if err := syncGroups(again, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(roundTrip(t, again), roundTrip(t, config)) {
		t.Errorf("second sync changed config:\n got %v\nwant %v", dnsRules(t, again), dnsRules(t, config))
	}
}

func TestSyncGroupsSplitsRuleSets(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)

	if err := syncGroups(config, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	route := config["route"].(map[string]any)

	var tags []string

	for _, item := range route["rule_set"].([]any) {
		ruleSet := item.(map[string]any)
		tags = append(tags, ruleSet["tag"].(string))

		if ruleSet["tag"] == "configurer-default@ip" && ruleSet["url"] != "http://127.0.0.1:8080/api/ruleset/ip?group=default" {
			t.Errorf("ip rule-set url = %v", ruleSet["url"])
		}

		if ruleSet["tag"] == "configurer-default" && ruleSet["url"] != "http://127.0.0.1:8080/api/ruleset/domain?group=default" {
			t.Errorf("domain rule-set url = %v", ruleSet["url"])
		}
	}

	wantTags := []string{
		"configurer-default", "configurer-default@ip",
		"configurer-claude", "configurer-claude@ip",
		"configurer-block", "configurer-block@ip",
	}

	if !slices.Equal(tags, wantTags) {
		t.Errorf("rule-set tags = %v, want %v", tags, wantTags)
	}

	var groupRules [][]string

	for _, item := range route["rules"].([]any) {
		rule := item.(map[string]any)

		if _, ok := rule["outbound"]; ok {
			groupRules = append(groupRules, extractRuleSetTags(rule))
		}
	}

	wantRules := [][]string{
		{"configurer-default", "configurer-default@ip"},
		{"configurer-claude", "configurer-claude@ip"},
		{"configurer-block", "configurer-block@ip"},
	}

	if !reflect.DeepEqual(groupRules, wantRules) {
		t.Errorf("route rules = %v, want %v", groupRules, wantRules)
	}
}

func TestSyncSingleGroupKeepsOtherDNSRules(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)

	// Синхронизируем только claude: ручное правило default остается как было, в том же месте.
	if err := syncGroups(config, testGroups[1:2], testGroups); err != nil {
		t.Fatal(err)
	}

	want := []string{
		`{"action":"predefined","query_type":["HTTPS"],"rcode":"NOERROR"}`,
		`{"rule_set":"configurer-default","server":"cloudflare"}`,
		`{"rule_set":"configurer-claude","server":"cloudflare"}`,
		`{"domain":["ha.home.lab"],"server":"static-hosts"}`,
		`{"domain":["claude.ai"],"server":"cloudflare"}`,
		`{"action":"route","preferred_by":["local"],"server":"local"}`,
	}

	if got := dnsRules(t, config); !slices.Equal(got, want) {
		t.Errorf("dns rules:\n got %v\nwant %v", got, want)
	}
}

func TestSyncGroupsRemovesDNSRules(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)

	if err := syncGroups(config, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	withoutDNS := slices.Clone(testGroups)

	for i := range withoutDNS {
		withoutDNS[i].DNSServer = ""
	}

	if err := syncGroups(config, withoutDNS, withoutDNS); err != nil {
		t.Fatal(err)
	}

	// Остаются только пользовательские правила, фильтр HTTPS-записей тоже убирается.
	want := []string{
		`{"domain":["ha.home.lab"],"server":"static-hosts"}`,
		`{"domain":["claude.ai"],"server":"cloudflare"}`,
		`{"action":"route","preferred_by":["local"],"server":"local"}`,
	}

	if got := dnsRules(t, config); !slices.Equal(got, want) {
		t.Errorf("dns rules:\n got %v\nwant %v", got, want)
	}
}

func TestSyncGroupsWithoutDNSSection(t *testing.T) {
	config := parseConfig(t, `{"route": {"rules": []}, "outbounds": []}`)

	groups := []Group{
		{
			Name:            "default",
			Description:     "",
			DefaultOutbound: "",
			DNSServer:       "",
		},
	}

	if err := syncGroups(config, groups, groups); err != nil {
		t.Fatal(err)
	}

	if _, ok := config["dns"]; ok {
		t.Error("dns section should not be created without dns servers")
	}
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

func TestCheckGroupsSyncDNS(t *testing.T) {
	provider := NewProvider("")
	path := filepath.Join(t.TempDir(), "config.json")

	if err := os.WriteFile(path, []byte(manualDNSConfig), 0644); err != nil {
		t.Fatal(err)
	}

	statuses, err := provider.CheckGroupsSync(path, testGroups)
	if err != nil {
		t.Fatal(err)
	}

	// До синхронизации rule-set-ы не разделены.
	for _, status := range statuses {
		if status.Synced {
			t.Errorf("group %s should not be synced before split", status.Name)
		}
	}

	if err = provider.SyncGroupsToConfig(path, testGroups); err != nil {
		t.Fatal(err)
	}

	statuses, err = provider.CheckGroupsSync(path, testGroups)
	if err != nil {
		t.Fatal(err)
	}

	for _, status := range statuses {
		if !status.DNSSynced {
			t.Errorf("group %s: dns not synced (actual %q, want %q)", status.Name, status.ActualDNSServer, status.DNSServer)
		}

		if !status.HasRuleSet || !status.HasRule {
			t.Errorf("group %s: rule-sets %v, rule %v", status.Name, status.HasRuleSet, status.HasRule)
		}
	}
}

// legacyEvaluateConfig — DNS-правила, которые создавала версия 0.4.0.
const legacyEvaluateConfig = `{
	"dns": {
		"rules": [
			{"action": "predefined", "query_type": ["HTTPS"], "rcode": "NOERROR"},
			{"rule_set": "configurer-default", "server": "cloudflare"},
			{"rule_set": "configurer-claude", "server": "cloudflare"},
			{"domain": ["ha.home.lab"], "server": "static-hosts"},
			{"action": "evaluate", "query_type": ["A", "AAAA"], "server": "cloudflare", "tag": "configurer-default@dns"},
			{"action": "respond", "match_response": "configurer-default@dns", "query_type": ["A", "AAAA"], "rule_set": "configurer-default@ip"},
			{"action": "evaluate", "query_type": ["A", "AAAA"], "server": "cloudflare", "tag": "configurer-claude@dns"},
			{"action": "respond", "match_response": "configurer-claude@dns", "query_type": ["A", "AAAA"], "rule_set": "configurer-claude@ip"}
		]
	},
	"route": {"rules": []},
	"outbounds": []
}`

func TestSyncGroupsRemovesLegacyEvaluate(t *testing.T) {
	config := parseConfig(t, legacyEvaluateConfig)

	// Синхронизация одной группы убирает только ее правила evaluate/respond.
	if err := syncGroups(config, testGroups[:1], testGroups); err != nil {
		t.Fatal(err)
	}

	if hasLegacyGroupDNSRules(config, "default") || !hasLegacyGroupDNSRules(config, "claude") {
		t.Errorf("after default sync: %v", dnsRules(t, config))
	}

	if err := syncGroups(config, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	want := []string{
		`{"action":"predefined","query_type":["HTTPS"],"rcode":"NOERROR"}`,
		`{"rule_set":"configurer-default","server":"cloudflare"}`,
		`{"rule_set":"configurer-claude","server":"cloudflare"}`,
		`{"domain":["ha.home.lab"],"server":"static-hosts"}`,
	}

	if got := dnsRules(t, config); !slices.Equal(got, want) {
		t.Errorf("dns rules:\n got %v\nwant %v", got, want)
	}
}

func TestCheckGroupsSyncDetectsLegacyEvaluate(t *testing.T) {
	provider := NewProvider("")
	path := filepath.Join(t.TempDir(), "config.json")

	config := parseConfig(t, legacyEvaluateConfig)

	if err := syncGroups(config, nil, testGroups); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	statuses, err := provider.CheckGroupsSync(path, testGroups)
	if err != nil {
		t.Fatal(err)
	}

	for _, status := range statuses {
		if status.Name != "block" && status.DNSSynced {
			t.Errorf("group %s: legacy evaluate rules must mark dns as not synced", status.Name)
		}
	}
}
