package rules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// newConflictsManager создает менеджер с группами first и second (в этом порядке), ручными правилами
// и загруженными источниками групп.
func newConflictsManager() *Manager {
	manager := newTestManager(
		[]Rule{
			{ID: "1", Type: "domain_suffix", Value: "example.com", Group: "first", Applied: true},
			{ID: "2", Type: "domain", Value: "www.test.org", Group: "second", Applied: true},
			{ID: "3", Type: "cidr", Value: "10.0.0.0/8", Group: "first", Applied: true},
			{ID: "4", Type: "ip", Value: "192.168.1.10", Group: "second", Applied: true},
			{ID: "5", Type: "domain", Value: "deleted.org", Group: "first", Applied: true, Deleted: true},
		},
		[]URLSource{
			{ID: "src-first", Description: "Список first", Group: "first", Applied: true},
			{ID: "src-second", Description: "Список second", Group: "second", Applied: true},
			{ID: "src-block", Description: "Блокировки", Group: BlockGroupName, Applied: true},
		},
		map[string]RuleSet{
			"src-first": {
				CidrList:       []string{"203.0.113.0/24"},
				Domains:        []string{"exact.io"},
				DomainSuffixes: []string{"video.net"},
			},
			"src-second": {
				CidrList:       []string{"198.51.100.0/24"},
				Domains:        []string{},
				DomainSuffixes: []string{"music.net"},
			},
			"src-block": {
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{"ads.example"},
			},
		},
	)

	manager.data.Groups = []Group{
		{Name: "first"},
		{Name: "second"},
	}

	return manager
}

// TestCheckRulesManualConflicts проверяет, что пересечения с ручными правилами любой группы запрещены.
func TestCheckRulesManualConflicts(t *testing.T) {
	manager := newConflictsManager()

	cases := []struct {
		ruleType string
		value    string
		group    string
		conflict bool
	}{
		{"domain", "api.example.com", "second", true},
		{"domain_suffix", "sub.example.com", "first", true},
		{"domain_suffix", "test.org", "first", true},
		{"domain", "WWW.Test.Org.", "first", true},
		{"domain", "deleted.org", "second", false},
		{"domain", "other.com", "second", false},
		{"ip", "10.1.2.3", "second", true},
		{"cidr", "10.20.0.0/16", "second", true},
		{"cidr", "192.168.1.0/24", "first", true},
		{"cidr", "0.0.0.0/0", "first", true},
		{"ip", "192.168.1.11", "first", false},
		{"cidr", "172.16.0.0/12", "first", false},
	}

	for _, tc := range cases {
		checks, err := manager.CheckRules(tc.ruleType, tc.group, []string{tc.value})
		if err != nil {
			t.Fatal(err)
		}

		if got := checks[0].Error != ""; got != tc.conflict {
			t.Errorf("%s %s в %s: conflict = %v (%q), want %v", tc.ruleType, tc.value, tc.group, got, checks[0].Error, tc.conflict)
		}
	}
}

// TestCheckRulesBetweenValues проверяет пересечения между значениями одной проверки (массовое добавление).
func TestCheckRulesBetweenValues(t *testing.T) {
	manager := newConflictsManager()

	checks, err := manager.CheckRules("cidr", "first", []string{"172.16.0.0/16", "172.16.5.0/24", "172.17.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	if checks[0].Error != "" || checks[1].Error == "" || checks[2].Error != "" {
		t.Errorf("checks = %+v", checks)
	}
}

// TestNormalizeRule проверяет приведение значений к виду хранения.
func TestNormalizeRule(t *testing.T) {
	cases := []struct {
		ruleType string
		value    string
		want     string
		invalid  bool
	}{
		{"domain", " WWW.Example.COM. ", "www.example.com", false},
		{"domain_suffix", "*.Example.com", "example.com", false},
		{"domain_suffix", ".example.com", "example.com", false},
		{"domain", "https://example.com", "", true},
		{"domain", "example.com:443", "", true},
		{"ip", "2001:DB8::1", "2001:db8::1", false},
		{"cidr", "10.1.2.3/8", "10.0.0.0/8", false},
		{"cidr", "192.168.1.1", "192.168.1.1", false},
		{"ip", "300.1.1.1", "", true},
	}

	for _, tc := range cases {
		rule := Rule{Type: tc.ruleType, Value: tc.value}

		err := normalizeRule(&rule)
		if tc.invalid {
			if err == nil {
				t.Errorf("%s %q must be invalid, got %q", tc.ruleType, tc.value, rule.Value)
			}

			continue
		}

		if err != nil || rule.Value != tc.want {
			t.Errorf("%s %q = %q (%v), want %q", tc.ruleType, tc.value, rule.Value, err, tc.want)
		}
	}
}

// TestCheckRulesSourceShadows проверяет предупреждения о значениях источников групп, которые стоят выше.
func TestCheckRulesSourceShadows(t *testing.T) {
	manager := newConflictsManager()

	cases := []struct {
		ruleType string
		value    string
		group    string
		warning  string
	}{
		// Источник группы first выше группы second.
		{"domain", "cdn.video.net", "second", "покрыт суффиксом video.net"},
		{"domain_suffix", "exact.io", "second", "для поддоменов сработает"},
		{"ip", "203.0.113.7", "second", "входит в 203.0.113.0/24"},
		{"cidr", "203.0.0.0/16", "second", "часть адресов"},

		// Группа нового правила выше группы источника: ручное правило переопределяет источник.
		{"domain", "cdn.music.net", "first", ""},
		{"ip", "198.51.100.1", "first", ""},

		// block срабатывает раньше всех групп, в том числе bypass.
		{"domain", "tracker.ads.example", "first", "группы block"},
		{"domain", "tracker.ads.example", BypassGroupName, "группы block"},
	}

	for _, tc := range cases {
		checks, err := manager.CheckRules(tc.ruleType, tc.group, []string{tc.value})
		if err != nil {
			t.Fatal(err)
		}

		check := checks[0]

		if check.Error != "" {
			t.Fatalf("%s %s: unexpected error %s", tc.ruleType, tc.value, check.Error)
		}

		warnings := strings.Join(check.Warnings, "; ")

		if tc.warning == "" && warnings != "" {
			t.Errorf("%s %s в %s: unexpected warnings %q", tc.ruleType, tc.value, tc.group, warnings)
		}

		if tc.warning != "" && !strings.Contains(warnings, tc.warning) {
			t.Errorf("%s %s в %s: warnings %q must contain %q", tc.ruleType, tc.value, tc.group, warnings, tc.warning)
		}
	}
}

// TestAddRuleNormalizesAndRejects проверяет, что добавление нормализует значение и отклоняет пересечения.
func TestAddRuleNormalizesAndRejects(t *testing.T) {
	manager, err := NewManager(appdata.NewFile(filepath.Join(t.TempDir(), "app.json")), Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err = manager.Load(); err != nil {
		t.Fatal(err)
	}

	added, _, err := manager.AddRule(Rule{ID: "1", Type: "cidr", Value: "10.1.2.3/16", Group: DefaultGroupName})
	if err != nil || added.Value != "10.1.0.0/16" {
		t.Fatalf("added = %+v, err = %v", added, err)
	}

	if _, _, err = manager.AddRule(Rule{ID: "2", Type: "ip", Value: "10.1.200.1", Group: BlockGroupName}); err == nil {
		t.Error("ip inside existing cidr must be rejected")
	}

	result, err := manager.AddRuleBulk("domain_suffix", "Example.com\nsub.example.com\n\nother.org", "", DefaultGroupName)
	if err != nil {
		t.Fatal(err)
	}

	if result.Success != 2 || result.Failed != 1 || result.AddedRules[0].Value != "example.com" {
		t.Errorf("bulk result = %+v", result)
	}
}
