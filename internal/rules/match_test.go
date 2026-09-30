package rules

import (
	"net/netip"
	"testing"
)

func TestMatchGroup(t *testing.T) {
	manager := newKindTestManager()

	tests := []struct {
		name   string
		kind   RuleSetKind
		domain string
		ip     string
		want   []Match
	}{
		{
			name:   "manual domain",
			kind:   RuleSetKindDomain,
			domain: "Claude.AI.",
			want:   []Match{manualMatch("domain", "claude.ai")},
		},
		{
			name:   "manual suffix covers subdomain",
			kind:   RuleSetKindAll,
			domain: "docs.sagernet.org",
			want:   []Match{manualMatch("domain_suffix", "sagernet.org")},
		},
		{
			name:   "url source suffix",
			kind:   RuleSetKindDomain,
			domain: "web.telegram.org",
			want:   []Match{{Type: "domain_suffix", Value: "telegram.org", Source: MatchSourceURL, SourceName: "Список"}},
		},
		{
			name: "manual and url cidr",
			kind: RuleSetKindIP,
			ip:   "149.154.167.99",
			want: []Match{{Type: "ip_cidr", Value: "149.154.160.0/20", Source: MatchSourceURL, SourceName: "Список"}},
		},
		{
			name:   "ip kind ignores domains",
			kind:   RuleSetKindIP,
			domain: "claude.ai",
			ip:     "31.13.64.5",
			want:   []Match{manualMatch("ip_cidr", "31.13.64.0/24")},
		},
		{
			name:   "no match",
			kind:   RuleSetKindAll,
			domain: "example.com",
			ip:     "8.8.8.8",
			want:   []Match{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ips []netip.Addr

			if tt.ip != "" {
				ips = []netip.Addr{netip.MustParseAddr(tt.ip)}
			}

			got := manager.MatchGroup("default", tt.kind, tt.domain, ips)

			if len(got) != len(tt.want) {
				t.Fatalf("matches = %+v, want %+v", got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("match %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMatchGroupSkipsConflictingSourceValues(t *testing.T) {
	manager := newTestManager(
		[]Rule{
			{ID: "1", Type: "domain", Value: "a.example.com", Group: "default", Applied: true},
		},
		[]URLSource{
			{ID: "src", Description: "Список", Group: "default", Applied: true},
		},
		map[string]RuleSet{
			"src": {CidrList: []string{}, Domains: []string{}, DomainSuffixes: []string{"example.com"}},
		},
	)

	// Суффикс источника конфликтует с ручным доменом и не попадает в набор — значит, и не совпадает.
	if got := manager.MatchGroup("default", RuleSetKindDomain, "b.example.com", nil); len(got) != 0 {
		t.Errorf("matches = %+v, want none", got)
	}

	if stats := manager.GroupStats("default"); stats.Domains != 1 || stats.Sources != 1 {
		t.Errorf("stats = %+v", stats)
	}
}
