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
				Bypass:  false,
				Applied: true,
			},
			{
				ID:      "2",
				Type:    "domain_suffix",
				Value:   "direct.ru",
				Group:   "default",
				Bypass:  true,
				Applied: true,
			},
			{
				ID:      "3",
				Type:    "cidr",
				Value:   "10.10.0.0/16",
				Group:   "claude",
				Bypass:  true,
				Applied: true,
			},
			{
				ID:      "4",
				Type:    "domain",
				Value:   "pending.ru",
				Group:   "default",
				Bypass:  true,
				Applied: false,
			},
		},
		[]URLSource{
			{
				ID:      "src-proxy",
				Group:   "default",
				Bypass:  false,
				Applied: true,
			},
			{
				ID:      "src-bypass",
				Group:   "claude",
				Bypass:  true,
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

	// В группе claude только исключения, поэтому ее набор пустой.
	if claude := mustRuleSet(t)(manager.GetRuleSetByGroup("claude", RuleSetKindAll)); len(claude.Rules) != 0 {
		t.Errorf("claude rules = %v, want empty", claude.Rules)
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

func TestAddGroupRejectsReservedName(t *testing.T) {
	manager := newTestManager([]Rule{}, []URLSource{}, map[string]RuleSet{})

	group := Group{
		Name:            ReservedGroupName,
		Description:     "",
		DefaultOutbound: "",
	}

	if err := manager.AddGroup(group); err == nil {
		t.Fatal("expected error for reserved group name")
	}
}
