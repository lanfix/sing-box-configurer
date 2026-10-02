package rules

import (
	"cmp"
	"fmt"
	"net/netip"
	"strings"
)

// RuleCheck — итог проверки значения нового правила.
type RuleCheck struct {
	// Value — значение в том виде, в котором оно будет сохранено.
	Value string `json:"value"`

	// Error — правило добавить нельзя: значение некорректно или пересекается с ручным правилом.
	Error string `json:"error,omitempty"`

	// Warnings — значение входит в URL-источники групп, которые стоят выше: для него сработают они.
	Warnings []string `json:"warnings,omitempty"`
}

// CheckRules проверяет значения новых правил типа ruleType группы group так же, как добавление: значения
// нормализуются, пересечения с ручными правилами (и между самими значениями) запрещены, пересечения
// с URL-источниками групп выше дают предупреждение. Данные не меняются.
func (rm *Manager) CheckRules(ruleType, group string, values []string) ([]RuleCheck, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if !rm.groupExists(group) {
		return nil, fmt.Errorf("группа %s не существует", group)
	}

	shadows := rm.newSourceShadows(group)
	accepted := make([]Rule, 0, len(values))
	checks := make([]RuleCheck, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)

		if value == "" {
			continue
		}

		rule := Rule{
			Type:  ruleType,
			Value: value,
			Group: group,
		}

		check := RuleCheck{
			Value:    value,
			Error:    "",
			Warnings: nil,
		}

		err := normalizeRule(&rule)
		if err == nil {
			err = checkConflicts(rule, rm.data.Rules)
		}

		if err == nil {
			err = checkConflicts(rule, accepted)
		}

		check.Value = rule.Value

		if err != nil {
			check.Error = err.Error()
		} else {
			accepted = append(accepted, rule)
			check.Warnings = shadows.check(rule)
		}

		checks = append(checks, check)
	}

	return checks, nil
}

// normalizeRule приводит значение правила к виду, в котором оно хранится, и проверяет его: домены — в нижнем
// регистре без точки в конце (у суффикса — и без «.» или «*.» в начале), IP и подсети — в каноническом виде.
func normalizeRule(rule *Rule) error {
	value := strings.TrimSpace(rule.Value)

	switch rule.Type {
	case "domain", "domain_suffix":
		value = strings.TrimSuffix(strings.ToLower(value), ".")

		if rule.Type == "domain_suffix" {
			value = strings.TrimPrefix(strings.TrimPrefix(value, "*."), ".")
		}

		if value == "" || strings.ContainsAny(value, " \t/\\:@*?#") {
			return fmt.Errorf("%s не является доменом", rule.Value)
		}

	case "ip", "cidr":
		prefix, err := parsePrefix(value)
		if err != nil {
			return err
		}

		// Одиночный адрес хранится без длины префикса, подсеть — с обнуленными битами хоста.
		if prefix.IsSingleIP() && !strings.Contains(value, "/") {
			value = prefix.Addr().String()
		} else {
			value = prefix.Masked().String()
		}

	default:
		return fmt.Errorf("неизвестный тип правила %s", rule.Type)
	}

	rule.Value = value

	return nil
}

// parsePrefix разбирает IP-адрес или подсеть. Адрес возвращается подсетью из одного адреса.
func parsePrefix(value string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix, nil
	}

	if addr, err := netip.ParseAddr(value); err == nil {
		addr = addr.WithZone("")

		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}

	return netip.Prefix{}, fmt.Errorf("%s не является IP-адресом или подсетью", value)
}

// checkConflicts проверяет, что правило не пересекается с правилами existing: повтор, домен под суффиксом,
// суффиксы друг под другом, адрес в подсети или пересекающиеся подсети. Правила, помеченные на удаление,
// не учитываются.
func checkConflicts(newRule Rule, existing []Rule) error {
	for _, rule := range existing {
		if rule.Deleted {
			continue
		}

		if err := conflictBetween(newRule, rule); err != nil {
			return err
		}
	}

	return nil
}

// conflictBetween возвращает ошибку, если правила newRule и rule пересекаются.
func conflictBetween(newRule, rule Rule) error {
	where := "в группе " + rule.Group

	if isIPRule(newRule.Type) != isIPRule(rule.Type) {
		return nil
	}

	if isIPRule(newRule.Type) {
		newPrefix, err := parsePrefix(newRule.Value)
		if err != nil {
			return nil
		}

		prefix, err := parsePrefix(rule.Value)
		if err != nil || !newPrefix.Overlaps(prefix) {
			return nil
		}

		switch {
		case newPrefix.Masked() == prefix.Masked():
			return fmt.Errorf("правило %s уже есть %s", rule.Value, where)

		case newPrefix.Bits() < prefix.Bits():
			return fmt.Errorf("подсеть %s включает %s, которое уже есть %s", newRule.Value, rule.Value, where)

		default:
			return fmt.Errorf("%s входит в подсеть %s, которая уже есть %s", newRule.Value, rule.Value, where)
		}
	}

	newValue, value := strings.ToLower(newRule.Value), strings.ToLower(rule.Value)

	switch {
	case newRule.Type == rule.Type && newValue == value:
		return fmt.Errorf("правило %s %s уже есть %s", newRule.Type, newRule.Value, where)

	case newRule.Type == "domain" && rule.Type == "domain_suffix" && isDomainMatchesSuffix(newValue, value):
		return fmt.Errorf("домен %s уже покрыт суффиксом %s %s", newRule.Value, rule.Value, where)

	case newRule.Type == "domain_suffix" && rule.Type == "domain" && isDomainMatchesSuffix(value, newValue):
		return fmt.Errorf("суффикс %s покрывает домен %s, который уже есть %s", newRule.Value, rule.Value, where)

	case newRule.Type == "domain_suffix" && rule.Type == "domain_suffix" && isDomainMatchesSuffix(newValue, value):
		return fmt.Errorf("суффикс %s уже покрыт суффиксом %s %s", newRule.Value, rule.Value, where)

	case newRule.Type == "domain_suffix" && rule.Type == "domain_suffix" && isDomainMatchesSuffix(value, newValue):
		return fmt.Errorf("суффикс %s покрывает суффикс %s, который уже есть %s", newRule.Value, rule.Value, where)
	}

	return nil
}

// isIPRule проверяет, что правило задает IP-адрес или подсеть.
func isIPRule(ruleType string) bool {
	return ruleType == "ip" || ruleType == "cidr"
}

// groupPriority возвращает место группы в порядке срабатывания: bypass (мимо туннеля, до маршрутизации),
// block, затем пользовательские группы в порядке создания. Меньше — раньше.
func (rm *Manager) groupPriority(name string) int {
	switch name {
	case BypassGroupName:
		return 0

	case BlockGroupName:
		return 1
	}

	for i, group := range rm.data.Groups {
		if group.Name == name {
			return i + 2
		}
	}

	return len(rm.data.Groups) + 2
}

// sourceShadows — значения URL-источников групп, которые срабатывают раньше группы нового правила.
type sourceShadows struct {
	domains  map[string]string
	suffixes map[string]string
	cidrs    []shadowCIDR
}

// shadowCIDR — подсеть URL-источника и его описание.
type shadowCIDR struct {
	prefix netip.Prefix
	source string
}

// newSourceShadows собирает значения URL-источников групп, которые стоят выше группы group. Значения
// подписаны источником и группой. Вызывается под rm.mu.
func (rm *Manager) newSourceShadows(group string) *sourceShadows {
	shadows := &sourceShadows{
		domains:  map[string]string{},
		suffixes: map[string]string{},
		cidrs:    []shadowCIDR{},
	}

	priority := rm.groupPriority(group)

	rm.urlRulesMu.RLock()
	defer rm.urlRulesMu.RUnlock()

	for _, source := range rm.data.URLSources {
		if source.Deleted || rm.groupPriority(source.Group) >= priority {
			continue
		}

		ruleSet, ok := rm.urlRules[source.ID]
		if !ok {
			continue
		}

		label := fmt.Sprintf("источник «%s» группы %s", cmp.Or(source.Description, source.URL), source.Group)

		for _, domain := range ruleSet.Domains {
			if domain = strings.ToLower(domain); shadows.domains[domain] == "" {
				shadows.domains[domain] = label
			}
		}

		for _, suffix := range ruleSet.DomainSuffixes {
			if suffix = strings.ToLower(suffix); shadows.suffixes[suffix] == "" {
				shadows.suffixes[suffix] = label
			}
		}

		for _, cidr := range ruleSet.CidrList {
			if prefix, err := parsePrefix(cidr); err == nil {
				shadows.cidrs = append(shadows.cidrs, shadowCIDR{
					prefix: prefix,
					source: label,
				})
			}
		}
	}

	return shadows
}

// check возвращает предупреждения о значениях источников групп выше, под которые попадает правило.
func (s *sourceShadows) check(rule Rule) []string {
	warnings := make([]string, 0)

	if isIPRule(rule.Type) {
		prefix, err := parsePrefix(rule.Value)
		if err != nil {
			return nil
		}

		for _, cidr := range s.cidrs {
			if !cidr.prefix.Overlaps(prefix) {
				continue
			}

			if cidr.prefix.Bits() <= prefix.Bits() {
				warnings = append(warnings, fmt.Sprintf("%s входит в %s (%s), группа выше — правило не сработает", rule.Value, cidr.prefix, cidr.source))
			} else {
				warnings = append(warnings, fmt.Sprintf("часть адресов %s входит в %s (%s), группа выше — для них правило не сработает", rule.Value, cidr.prefix, cidr.source))
			}

			if len(warnings) >= 3 {
				break
			}
		}

		return warnings
	}

	value := strings.ToLower(rule.Value)

	// Суффикс источника покрывает значение, если совпадает с ним или с одним из его родительских доменов.
	for parent := value; parent != ""; {
		if label, ok := s.suffixes[parent]; ok {
			return append(warnings, fmt.Sprintf("%s покрыт суффиксом %s (%s), группа выше — правило не сработает", rule.Value, parent, label))
		}

		_, rest, found := strings.Cut(parent, ".")
		if !found {
			break
		}

		parent = rest
	}

	if label, ok := s.domains[value]; ok {
		if rule.Type == "domain" {
			return append(warnings, fmt.Sprintf("%s есть в %s, группа выше — правило не сработает", rule.Value, label))
		}

		warnings = append(warnings, fmt.Sprintf("домен %s есть в %s, группа выше — для него правило не сработает, для поддоменов сработает", rule.Value, label))
	}

	return warnings
}
