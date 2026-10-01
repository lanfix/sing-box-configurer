package rules

import (
	"slices"
	"testing"
)

func TestRowHandler(t *testing.T) {
	tests := []struct {
		name     string
		row      string
		expected RuleSet
	}{
		{
			name: "empty row",
			row:  "",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "comment with hash",
			row:  "# This is a comment",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "comment with slashes",
			row:  "// This is a comment",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "full domain",
			row:  "full:discordcdn.com",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{"discordcdn.com"},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "full domain with spaces",
			row:  "  full:discordcdn.com  ",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{"discordcdn.com"},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "full domain empty",
			row:  "full:",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "CIDR",
			row:  "192.168.1.0/24",
			expected: RuleSet{
				CidrList:       []string{"192.168.1.0/24"},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "CIDR with spaces",
			row:  "  192.168.1.0/24  ",
			expected: RuleSet{
				CidrList:       []string{"192.168.1.0/24"},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "CIDR IPv6",
			row:  "2001:db8::/32",
			expected: RuleSet{
				CidrList:       []string{"2001:db8::/32"},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "invalid CIDR",
			row:  "192.168.1.0/33",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "single IP",
			row:  "192.168.1.1",
			expected: RuleSet{
				CidrList:       []string{"192.168.1.1/32"},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "single IPv6",
			row:  "2001:db8::1",
			expected: RuleSet{
				CidrList:       []string{"2001:db8::1/128"},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "domain suffix with dot",
			row:  ".google.com",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{"google.com"},
			},
		},
		{
			name: "domain suffix with spaces",
			row:  "  .google.com  ",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{"google.com"},
			},
		},
		{
			name: "domain suffix empty after dot",
			row:  ".",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "regular domain",
			row:  "discord.com",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{"discord.com"},
			},
		},
		{
			name: "domain with subdomain",
			row:  "api.discord.com",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{"api.discord.com"},
			},
		},
		{
			name: "domain with spaces",
			row:  "  discord.com  ",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{"discord.com"},
			},
		},
		{
			name: "domain without dot",
			row:  "localhost",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			},
		},
		{
			name: "full domain takes precedence over domain",
			row:  "full:discord.com",
			expected: RuleSet{
				CidrList:       []string{},
				Domains:        []string{"discord.com"},
				DomainSuffixes: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &RuleSet{
				CidrList:       []string{},
				Domains:        []string{},
				DomainSuffixes: []string{},
			}

			if err := rowHandler(tt.row, rs); err != nil {
				t.Errorf("rowHandler() returned error: %v", err)

				return
			}

			if !slices.Equal(rs.CidrList, tt.expected.CidrList) {
				t.Errorf("CidrList = %v, want %v", rs.CidrList, tt.expected.CidrList)
			}

			if !slices.Equal(rs.Domains, tt.expected.Domains) {
				t.Errorf("Domains = %v, want %v", rs.Domains, tt.expected.Domains)
			}

			if !slices.Equal(rs.DomainSuffixes, tt.expected.DomainSuffixes) {
				t.Errorf("DomainSuffixes = %v, want %v", rs.DomainSuffixes, tt.expected.DomainSuffixes)
			}
		})
	}
}
