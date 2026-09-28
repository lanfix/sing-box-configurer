package rules

import (
	"context"
	"slices"
	"testing"
)

// newTestManager создает менеджер с правилами и загруженными источниками без обращения к диску.
func newTestManager(rules []Rule, sources []URLSource, urlRules map[string]RuleSet) *Manager {
	return &Manager{
		urlRules:    urlRules,
		cancelFuncs: map[string]context.CancelFunc{},
		data: RulesData{
			Rules:      rules,
			URLSources: sources,
			Groups:     []Group{},
		},
	}
}

// ruleSetValues возвращает значения поля key единственного правила набора.
func ruleSetValues(t *testing.T, ruleSet SingBoxRuleSet, key string) []string {
	t.Helper()

	if len(ruleSet.Rules) == 0 {
		return nil
	}

	if len(ruleSet.Rules) != 1 {
		t.Fatalf("expected single rule, got %d", len(ruleSet.Rules))
	}

	values, _ := ruleSet.Rules[0][key].([]string)

	return values
}

// mustRuleSet возвращает функцию, которая проверяет, что набор собран без ошибки.
func mustRuleSet(t *testing.T) func(SingBoxRuleSet, error) SingBoxRuleSet {
	t.Helper()

	return func(ruleSet SingBoxRuleSet, err error) SingBoxRuleSet {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}

		return ruleSet
	}
}

func TestGetRuleSetSplitsBypass(t *testing.T) {
	manager := newTestManager(
		[]Rule{
			{
				ID:      "1",
				Type:    "domain_suffix",
				Value:   "proxied.com",
				Group:   "default",
				Applied: true,
			},
			{
				ID:      "2",
				Type:    "domain_suffix",
				Value:   "direct.ru",
				Group:   BypassGroupName,
				Applied: true,
			},
			{
				ID:      "3",
				Type:    "cidr",
				Value:   "10.10.0.0/16",
				Group:   BypassGroupName,
				Applied: true,
			},
			{
				ID:      "4",
				Type:    "domain",
				Value:   "pending.ru",
				Group:   BypassGroupName,
				Applied: false,
			},
		},
		[]URLSource{
			{
				ID:      "src-proxy",
				Group:   "default",
				Applied: true,
			},
			{
				ID:      "src-bypass",
				Group:   BypassGroupName,
				Applied: true,
			},
		},
		map[string]RuleSet{
			"src-proxy": {
				CidrList:       []string{"1.1.1.0/24"},
				Domains:        []string{},
				DomainSuffixes: []string{"proxy-list.com"},
			},
			"src-bypass": {
				CidrList:       []string{"5.255.255.0/24"},
				Domains:        []string{},
				DomainSuffixes: []string{"yandex.ru", "sub.direct.ru"},
			},
		},
	)

	group := mustRuleSet(t)(manager.GetRuleSetByGroup("default", RuleSetKindAll))

	if got, want := ruleSetValues(t, group, "domain_suffix"), []string{"proxied.com", "proxy-list.com"}; !slices.Equal(got, want) {
		t.Errorf("default domain_suffix = %v, want %v", got, want)
	}

	if got, want := ruleSetValues(t, group, "ip_cidr"), []string{"1.1.1.0/24"}; !slices.Equal(got, want) {
		t.Errorf("default ip_cidr = %v, want %v", got, want)
	}

	bypass := mustRuleSet(t)(manager.GetBypassRuleSet())

	// sub.direct.ru из источника перекрыт ручным суффиксом direct.ru, pending.ru еще не применен.
	if got, want := ruleSetValues(t, bypass, "domain_suffix"), []string{"direct.ru", "yandex.ru"}; !slices.Equal(got, want) {
		t.Errorf("bypass domain_suffix = %v, want %v", got, want)
	}

	if got, want := ruleSetValues(t, bypass, "ip_cidr"), []string{"10.10.0.0/16", "5.255.255.0/24"}; !slices.Equal(got, want) {
		t.Errorf("bypass ip_cidr = %v, want %v", got, want)
	}

	if got := ruleSetValues(t, bypass, "domain"); len(got) != 0 {
		t.Errorf("bypass domain = %v, want empty", got)
	}
}

func TestEmptyRuleSetHasNoNullRules(t *testing.T) {
	manager := newTestManager([]Rule{}, []URLSource{}, map[string]RuleSet{})

	ruleSet := mustRuleSet(t)(manager.GetBypassRuleSet())

	if ruleSet.Rules == nil {
		t.Fatal("rules should be empty slice, not nil")
	}
}

func TestSystemGroups(t *testing.T) {
	manager := newTestManager([]Rule{}, []URLSource{}, map[string]RuleSet{})

	for _, name := range []string{BlockGroupName, BypassGroupName} {
		group := Group{
			Name:            name,
			Description:     "",
			DefaultOutbound: "",
		}

		if err := manager.AddGroup(group); err == nil {
			t.Errorf("%s: expected error for system group name", name)
		}

		if err := manager.DeleteGroup(name); err == nil {
			t.Errorf("%s: system group must not be deleted", name)
		}

		if !manager.groupExists(name) {
			t.Errorf("%s: system group must exist", name)
		}
	}

	if err := manager.AddGroup(Group{Name: "bad name"}); err == nil {
		t.Error("expected error for invalid group name")
	}
}
