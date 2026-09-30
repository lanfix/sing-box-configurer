package rules

import (
	"net/netip"
	"strings"
)

// Источники совпадения правила.
const (
	// MatchSourceManual — совпало правило, добавленное вручную.
	MatchSourceManual = "manual"

	// MatchSourceURL — совпало значение из URL-источника.
	MatchSourceURL = "url"
)

// Match — правило группы, под которое попал домен или IP.
type Match struct {
	// Type — вид правила: domain, domain_suffix или ip_cidr.
	Type  string `json:"type"`
	Value string `json:"value"`

	// Source — откуда правило: manual или url, SourceName — описание URL-источника.
	Source     string `json:"source"`
	SourceName string `json:"source_name,omitempty"`
}

// GroupStats — сколько значений в наборах группы.
type GroupStats struct {
	Domains  int `json:"domains"`
	Suffixes int `json:"suffixes"`
	IPs      int `json:"ips"`

	// Sources — URL-источники группы, SourceItems — сколько значений они принесли.
	Sources     int    `json:"sources"`
	SourceItems uint64 `json:"source_items"`
}

// maxMatches ограничивает число совпадений, которые возвращает MatchGroup.
const maxMatches = 20

// MatchGroup возвращает примененные правила группы groupName из набора вида kind, под которые попадает
// домен domain или один из адресов ips. Значения URL-источников, отсеченные при сборке набора из-за
// конфликта с ручными правилами группы, не учитываются — как и в наборе, который получает sing-box.
func (rm *Manager) MatchGroup(groupName string, kind RuleSetKind, domain string, ips []netip.Addr) []Match {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	domain = strings.TrimSuffix(strings.ToLower(domain), ".")
	matches := make([]Match, 0)
	manualDomains := make(map[string]bool)
	manualSuffixes := make(map[string]bool)

	for _, rule := range rm.data.Rules {
		if !rule.Applied || rule.Group != groupName {
			continue
		}

		switch rule.Type {
		case "domain":
			manualDomains[rule.Value] = true

			if kind.includesDomains() && domain != "" && strings.EqualFold(rule.Value, domain) {
				matches = append(matches, manualMatch("domain", rule.Value))
			}

		case "domain_suffix":
			manualSuffixes[rule.Value] = true

			if kind.includesDomains() && domain != "" && matchSuffix(domain, rule.Value) {
				matches = append(matches, manualMatch("domain_suffix", rule.Value))
			}

		case "ip", "cidr":
			if kind.includesIPs() && matchCIDR(rule.Value, ips) {
				matches = append(matches, manualMatch("ip_cidr", rule.Value))
			}
		}
	}

	rm.urlRulesMu.RLock()
	defer rm.urlRulesMu.RUnlock()

	for _, source := range rm.data.URLSources {
		if len(matches) >= maxMatches {
			break
		}

		if !source.Applied || source.Group != groupName {
			continue
		}

		ruleSet, ok := rm.urlRules[source.ID]
		if !ok {
			continue
		}

		matches = append(matches, matchURLRuleSet(ruleSet, source.Description, kind, domain, ips, manualDomains, manualSuffixes)...)
	}

	if len(matches) > maxMatches {
		matches = matches[:maxMatches]
	}

	return matches
}

// matchURLRuleSet возвращает значения набора URL-источника, под которые попадает домен или IP.
func matchURLRuleSet(
	ruleSet RuleSet, sourceName string, kind RuleSetKind, domain string, ips []netip.Addr,
	manualDomains, manualSuffixes map[string]bool,
) []Match {
	matches := make([]Match, 0)

	urlMatch := func(ruleType, value string) Match {
		return Match{
			Type:       ruleType,
			Value:      value,
			Source:     MatchSourceURL,
			SourceName: sourceName,
		}
	}

	if kind.includesDomains() && domain != "" {
		for _, value := range ruleSet.Domains {
			if !strings.EqualFold(value, domain) || manualDomains[value] {
				continue
			}

			if hasConflictWithManualRules(value, "domain", manualDomains, manualSuffixes) {
				continue
			}

			matches = append(matches, urlMatch("domain", value))
		}

		for _, value := range ruleSet.DomainSuffixes {
			if !matchSuffix(domain, value) || manualSuffixes[value] {
				continue
			}

			if hasConflictWithManualRules(value, "domain_suffix", manualDomains, manualSuffixes) {
				continue
			}

			matches = append(matches, urlMatch("domain_suffix", value))
		}
	}

	if kind.includesIPs() && len(ips) > 0 {
		for _, value := range ruleSet.CidrList {
			if matchCIDR(value, ips) {
				matches = append(matches, urlMatch("ip_cidr", value))
			}
		}
	}

	return matches
}

// GroupStats возвращает размеры наборов группы groupName: ручные правила по видам и URL-источники.
func (rm *Manager) GroupStats(groupName string) GroupStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := GroupStats{
		Domains:     0,
		Suffixes:    0,
		IPs:         0,
		Sources:     0,
		SourceItems: 0,
	}

	for _, rule := range rm.data.Rules {
		if !rule.Applied || rule.Group != groupName {
			continue
		}

		switch rule.Type {
		case "domain":
			stats.Domains++

		case "domain_suffix":
			stats.Suffixes++

		case "ip", "cidr":
			stats.IPs++
		}
	}

	for _, source := range rm.data.URLSources {
		if !source.Applied || source.Group != groupName {
			continue
		}

		stats.Sources++
		stats.SourceItems += source.ItemsCount
	}

	return stats
}

// manualMatch возвращает совпадение с ручным правилом.
func manualMatch(ruleType, value string) Match {
	return Match{
		Type:       ruleType,
		Value:      value,
		Source:     MatchSourceManual,
		SourceName: "",
	}
}

// matchSuffix проверяет домен по суффиксу так же, как domain_suffix в sing-box: суффикс с точкой в начале
// совпадает только с поддоменами, без точки — с самим доменом и его поддоменами.
func matchSuffix(domain, suffix string) bool {
	suffix = strings.ToLower(suffix)

	if strings.HasPrefix(suffix, ".") {
		return strings.HasSuffix(domain, suffix)
	}

	return isDomainMatchesSuffix(domain, suffix)
}

// matchCIDR проверяет, попадает ли один из адресов ips в подсеть или адрес value.
func matchCIDR(value string, ips []netip.Addr) bool {
	if len(ips) == 0 {
		return false
	}

	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		addr, addrErr := netip.ParseAddr(value)
		if addrErr != nil {
			return false
		}

		prefix = netip.PrefixFrom(addr, addr.BitLen())
	}

	for _, ip := range ips {
		if prefix.Contains(ip.Unmap()) {
			return true
		}
	}

	return false
}
