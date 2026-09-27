package rules

import (
	"errors"
	"slices"
	"testing"
)

// newKindTestManager создает менеджер с доменом, суффиксом и CIDR в группе default и загруженным источником.
func newKindTestManager() *Manager {
	return newTestManager(
		[]Rule{
			{
				ID:      "1",
				Type:    "domain",
				Value:   "claude.ai",
				Group:   "default",
				Applied: true,
			},
			{
				ID:      "2",
				Type:    "domain_suffix",
				Value:   "sagernet.org",
				Group:   "default",
				Applied: true,
			},
			{
				ID:      "3",
				Type:    "cidr",
				Value:   "31.13.64.0/24",
				Group:   "default",
				Applied: true,
			},
		},
		[]URLSource{
			{
				ID:          "src",
				Description: "Список",
				Group:       "default",
				Applied:     true,
			},
		},
		map[string]RuleSet{
			"src": {
				CidrList:       []string{"149.154.160.0/20"},
				Domains:        []string{},
				DomainSuffixes: []string{"telegram.org"},
			},
		},
	)
}

func TestGetRuleSetByGroupKind(t *testing.T) {
	manager := newKindTestManager()

	domain := mustRuleSet(t)(manager.GetRuleSetByGroup("default", RuleSetKindDomain))

	if got, want := ruleSetValues(t, domain, "domain"), []string{"claude.ai"}; !slices.Equal(got, want) {
		t.Errorf("domain kind domain = %v, want %v", got, want)
	}

	if got, want := ruleSetValues(t, domain, "domain_suffix"), []string{"sagernet.org", "telegram.org"}; !slices.Equal(got, want) {
		t.Errorf("domain kind domain_suffix = %v, want %v", got, want)
	}

	// Набор доменов не должен содержать ip_cidr, иначе DNS-правило с ним станет legacy address filter.
	if got := ruleSetValues(t, domain, "ip_cidr"); len(got) != 0 {
		t.Errorf("domain kind ip_cidr = %v, want empty", got)
	}

	ip := mustRuleSet(t)(manager.GetRuleSetByGroup("default", RuleSetKindIP))

	if got, want := ruleSetValues(t, ip, "ip_cidr"), []string{"31.13.64.0/24", "149.154.160.0/20"}; !slices.Equal(got, want) {
		t.Errorf("ip kind ip_cidr = %v, want %v", got, want)
	}

	if got := ruleSetValues(t, ip, "domain_suffix"); len(got) != 0 {
		t.Errorf("ip kind domain_suffix = %v, want empty", got)
	}

	all := mustRuleSet(t)(manager.GetRuleSetByGroup("default", RuleSetKindAll))

	if got, want := ruleSetValues(t, all, "ip_cidr"), []string{"31.13.64.0/24", "149.154.160.0/20"}; !slices.Equal(got, want) {
		t.Errorf("all kind ip_cidr = %v, want %v", got, want)
	}

	if got, want := ruleSetValues(t, all, "domain_suffix"), []string{"sagernet.org", "telegram.org"}; !slices.Equal(got, want) {
		t.Errorf("all kind domain_suffix = %v, want %v", got, want)
	}
}

func TestEmptyIPRuleSetHasNoNullRules(t *testing.T) {
	manager := newTestManager(
		[]Rule{
			{
				ID:      "1",
				Type:    "domain",
				Value:   "claude.ai",
				Group:   "claude",
				Applied: true,
			},
		},
		[]URLSource{},
		map[string]RuleSet{},
	)

	ruleSet := mustRuleSet(t)(manager.GetRuleSetByGroup("claude", RuleSetKindIP))

	if ruleSet.Rules == nil || len(ruleSet.Rules) != 0 {
		t.Fatalf("rules = %v, want empty slice", ruleSet.Rules)
	}
}

func TestGetRuleSetNotReadyUntilSourceLoaded(t *testing.T) {
	manager := newKindTestManager()

	// Источник применен, но после старта еще не загружен.
	delete(manager.urlRules, "src")

	for _, kind := range []RuleSetKind{RuleSetKindAll, RuleSetKindDomain, RuleSetKindIP} {
		if _, err := manager.GetRuleSetByGroup("default", kind); !errors.Is(err, ErrRuleSetNotReady) {
			t.Errorf("kind %q: err = %v, want ErrRuleSetNotReady", kind, err)
		}
	}

	// Незагруженный источник другой группы не мешает отдавать набор.
	if _, err := manager.GetRuleSetByGroup("claude", RuleSetKindAll); err != nil {
		t.Errorf("claude: unexpected error %v", err)
	}

	if _, err := manager.GetBypassRuleSet(); err != nil {
		t.Errorf("bypass: unexpected error %v", err)
	}
}
