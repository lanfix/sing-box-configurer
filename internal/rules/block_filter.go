package rules

import (
	"net/netip"
	"slices"
	"strings"
)

// blockFilter — значения набора block. Группа block срабатывает раньше bypass, поэтому из набора bypass
// убирается все, что покрывает block: иначе такие адреса ушли бы мимо туннеля и не были бы отклонены.
type blockFilter struct {
	domains  map[string]bool
	suffixes map[string]bool

	// prefixes — подсети block с обнуленными битами хоста, отсортированные по начальному адресу.
	prefixes []netip.Prefix
}

// newBlockFilterLocked собирает фильтр из набора, который sing-box получает для группы block. Вызывается
// под rm.mu, urlRulesMu должен быть свободен.
func (rm *Manager) newBlockFilterLocked() (*blockFilter, error) {
	ruleSet, err := rm.getRuleSetLocked(RuleSetKindAll, BlockGroupName)
	if err != nil {
		return nil, err
	}

	filter := &blockFilter{
		domains:  map[string]bool{},
		suffixes: map[string]bool{},
		prefixes: []netip.Prefix{},
	}

	for _, domain := range ruleSetField(ruleSet, "domain") {
		filter.domains[strings.ToLower(domain)] = true
	}

	for _, suffix := range ruleSetField(ruleSet, "domain_suffix") {
		filter.suffixes[strings.ToLower(suffix)] = true
	}

	for _, cidr := range ruleSetField(ruleSet, "ip_cidr") {
		if prefix, err := parsePrefix(cidr); err == nil {
			filter.prefixes = append(filter.prefixes, prefix.Masked())
		}
	}

	slices.SortFunc(filter.prefixes, comparePrefixes)

	return filter, nil
}

// apply убирает из набора bypass домены и суффиксы, целиком покрытые block, и вычитает подсети block из
// подсетей bypass. Суффикс bypass, внутри которого block отклоняет только часть доменов, остается: такие
// домены не резолвятся DNS-правилом block, и их адреса не попадают в исключения туннеля.
func (f *blockFilter) apply(ruleSet SingBoxRuleSet) SingBoxRuleSet {
	if len(ruleSet.Rules) == 0 {
		return ruleSet
	}

	rule := ruleSet.Rules[0]

	filterField(rule, "domain", func(domain string) bool {
		domain = strings.ToLower(domain)

		return f.domains[domain] || f.coversSuffix(domain, false)
	})

	filterField(rule, "domain_suffix", func(suffix string) bool {
		suffix = strings.ToLower(suffix)

		return f.coversSuffix(strings.TrimPrefix(suffix, "."), strings.HasPrefix(suffix, "."))
	})

	if cidrs := ruleSetField(ruleSet, "ip_cidr"); len(cidrs) > 0 {
		setField(rule, "ip_cidr", f.subtract(cidrs))
	}

	if len(rule) == 0 {
		ruleSet.Rules = ruleSet.Rules[:0]
	}

	return ruleSet
}

// coversSuffix проверяет, что суффиксы block покрывают домен base целиком. Если subdomainsOnly, то
// проверяются только поддомены base (суффикс вида «.example.com»).
func (f *blockFilter) coversSuffix(base string, subdomainsOnly bool) bool {
	for parent := base; parent != ""; {
		// Суффикс без точки покрывает сам домен и поддомены, с точкой — только поддомены.
		if f.suffixes[parent] || (f.suffixes["."+parent] && (parent != base || subdomainsOnly)) {
			return true
		}

		_, rest, found := strings.Cut(parent, ".")
		if !found {
			break
		}

		parent = rest
	}

	return false
}

// coversMatch проверяет, что совпадение соединения с правилом bypass не сработает из-за block: домен
// соединения domain отклоняется block, либо все адреса из ips, попавшие в подсеть правила, отклоняются block.
func (f *blockFilter) coversMatch(match Match, domain string, ips []netip.Addr) bool {
	switch match.Type {
	case "domain", "domain_suffix":
		return f.domains[domain] || f.coversSuffix(domain, false)

	case "ip_cidr":
		for _, ip := range ips {
			addr := ip.Unmap()

			if matchCIDR(match.Value, []netip.Addr{addr}) && !f.containsPrefix(netip.PrefixFrom(addr, addr.BitLen())) {
				return false
			}
		}

		return true
	}

	return false
}

// subtract вычитает подсети block из подсетей cidrs. Подсети, не задетые block, остаются в исходной записи,
// задетые — заменяются оставшимися частями, покрытые целиком — убираются.
func (f *blockFilter) subtract(cidrs []string) []string {
	if len(f.prefixes) == 0 {
		return cidrs
	}

	result := make([]string, 0, len(cidrs))

	for _, cidr := range cidrs {
		prefix, err := parsePrefix(cidr)
		if err != nil {
			result = append(result, cidr)

			continue
		}

		prefix = prefix.Masked()

		if f.containsPrefix(prefix) {
			continue
		}

		inner := f.innerPrefixes(prefix)

		if len(inner) == 0 {
			result = append(result, cidr)

			continue
		}

		for _, part := range splitExcluding(prefix, inner) {
			result = append(result, part.String())
		}
	}

	return result
}

// containsPrefix проверяет, что подсеть prefix целиком входит в одну из подсетей block.
func (f *blockFilter) containsPrefix(prefix netip.Prefix) bool {
	for bits := prefix.Bits(); bits >= 0; bits-- {
		parent := netip.PrefixFrom(prefix.Addr(), bits).Masked()

		if _, found := slices.BinarySearchFunc(f.prefixes, parent, comparePrefixes); found {
			return true
		}
	}

	return false
}

// innerPrefixes возвращает подсети block, которые лежат внутри prefix. Подсети либо вложены, либо
// не пересекаются, поэтому достаточно найти те, что начинаются внутри prefix.
func (f *blockFilter) innerPrefixes(prefix netip.Prefix) []netip.Prefix {
	start, _ := slices.BinarySearchFunc(f.prefixes, prefix, comparePrefixes)
	inner := make([]netip.Prefix, 0)

	for _, candidate := range f.prefixes[start:] {
		if !prefix.Contains(candidate.Addr()) {
			break
		}

		inner = append(inner, candidate)
	}

	return inner
}

// splitExcluding делит prefix пополам, пока части не перестанут пересекаться с exclude, и возвращает части
// без адресов exclude. Все подсети exclude лежат внутри prefix.
func splitExcluding(prefix netip.Prefix, exclude []netip.Prefix) []netip.Prefix {
	if len(exclude) == 0 {
		return []netip.Prefix{prefix}
	}

	if slices.ContainsFunc(exclude, func(candidate netip.Prefix) bool { return candidate.Bits() <= prefix.Bits() }) {
		return nil
	}

	low := netip.PrefixFrom(prefix.Addr(), prefix.Bits()+1)
	high := netip.PrefixFrom(setBit(prefix.Addr(), prefix.Bits()), prefix.Bits()+1)

	lowExclude := make([]netip.Prefix, 0, len(exclude))
	highExclude := make([]netip.Prefix, 0, len(exclude))

	for _, candidate := range exclude {
		if low.Contains(candidate.Addr()) {
			lowExclude = append(lowExclude, candidate)
		} else {
			highExclude = append(highExclude, candidate)
		}
	}

	return append(splitExcluding(low, lowExclude), splitExcluding(high, highExclude)...)
}

// setBit возвращает адрес addr с выставленным битом номер bit (считая от старшего).
func setBit(addr netip.Addr, bit int) netip.Addr {
	bytes := addr.AsSlice()
	bytes[bit/8] |= 0x80 >> (bit % 8)

	result, _ := netip.AddrFromSlice(bytes)

	return result
}

// comparePrefixes упорядочивает подсети по начальному адресу, при равном — по длине префикса.
func comparePrefixes(a, b netip.Prefix) int {
	if c := a.Addr().Compare(b.Addr()); c != 0 {
		return c
	}

	return a.Bits() - b.Bits()
}

// ruleSetField возвращает значения поля key единственного правила набора.
func ruleSetField(ruleSet SingBoxRuleSet, key string) []string {
	if len(ruleSet.Rules) == 0 {
		return nil
	}

	values, _ := ruleSet.Rules[0][key].([]string)

	return values
}

// filterField убирает из поля key правила rule значения, для которых drop возвращает true.
func filterField(rule map[string]any, key string, drop func(string) bool) {
	values, _ := rule[key].([]string)

	if len(values) == 0 {
		return
	}

	setField(rule, key, slices.DeleteFunc(slices.Clone(values), drop))
}

// setField записывает значения в поле key правила rule, пустой список убирает поле.
func setField(rule map[string]any, key string, values []string) {
	if len(values) == 0 {
		delete(rule, key)

		return
	}

	rule[key] = values
}
