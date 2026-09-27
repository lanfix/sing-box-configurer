package singboxconfig

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// ipRuleSetTagSuffix — суффикс тега rule-set-а с IP/CIDR группы. Символ @ недопустим в именах групп,
	// поэтому тег не пересекается с тегами других групп.
	ipRuleSetTagSuffix = "@ip"

	// dnsEvaluateTagSuffix — суффикс тега evaluate-ответа группы. Такие правила создавала версия 0.4.0,
	// теперь они только удаляются при синхронизации.
	dnsEvaluateTagSuffix = "@dns"
)

// groupDNSRules — системные DNS-правила одной группы.
type groupDNSRules struct {
	// head — правила в начале dns.rules (домены группы).
	head []any

	// legacy — правила evaluate/respond в конце dns.rules, созданные версией 0.4.0.
	legacy []any
}

// syncDNSRules приводит системные DNS-правила групп targets к желаемому состоянию. Системные правила
// остальных групп из allGroups сохраняются как есть, пользовательские правила не трогаются.
//
// Для группы с DNS-сервером в начале dns.rules создается правило: запросы к доменам группы уходят на ее
// сервер. Остальные запросы, в том числе к адресам из IP/CIDR групп, идут дальше, к dns.final.
// Правила evaluate/respond, оставшиеся от версии 0.4.0, у групп targets удаляются.
//
// Пока DNS-правила есть хотя бы у одной группы, первым добавляется фильтр HTTPS-записей: без ECH-ключей
// клиенты отправляют настоящий SNI, и sniff маршрутизирует соединение по домену, а не по cloudflare-ech.com.
func syncDNSRules(config map[string]any, targets []Group, allGroups []Group) error {
	_, hasDNS := config["dns"]
	hasTargetServer := slices.ContainsFunc(targets, func(group Group) bool {
		return group.DNSServer != ""
	})

	// Секцию dns не создаем, если управлять в ней нечем.
	if !hasDNS && !hasTargetServer {
		return nil
	}

	dns, err := getOrCreateMap(config, "dns")
	if err != nil {
		return err
	}

	rules, err := getSlice(dns, "rules")
	if err != nil {
		return fmt.Errorf(".dns: %w", err)
	}

	userRules := make([]any, 0, len(rules))
	hostsRules := make([]any, 0, 1)
	systemRules := map[string]*groupDNSRules{}

	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			userRules = append(userRules, item)

			continue
		}

		// Правило DNS-записей конфигуратора всегда остается первым.
		if isHostsRule(rule) {
			hostsRules = append(hostsRules, rule)

			continue
		}

		// Фильтр HTTPS-записей пересоздается ниже, если он нужен.
		if isHTTPSFilterRule(rule) {
			continue
		}

		groupName, isHead, ok := parseSystemDNSRule(rule)
		if !ok {
			userRules = append(userRules, item)

			continue
		}

		groupRules, ok := systemRules[groupName]
		if !ok {
			groupRules = &groupDNSRules{
				head:   []any{},
				legacy: []any{},
			}
			systemRules[groupName] = groupRules
		}

		if isHead {
			groupRules.head = append(groupRules.head, rule)
		} else {
			groupRules.legacy = append(groupRules.legacy, rule)
		}
	}

	for _, group := range targets {
		systemRules[group.Name] = newGroupDNSRules(group)
	}

	head := make([]any, 0)
	tail := make([]any, 0)

	// Выстраиваем системные правила в порядке из allGroups. Правила удаленных групп отбрасываются.
	for _, group := range allGroups {
		groupRules, ok := systemRules[group.Name]
		if !ok {
			continue
		}

		head = append(head, groupRules.head...)
		tail = append(tail, groupRules.legacy...)
	}

	// Фильтр системный: созданный ранее конфигуратором не отличить от добавленного вручную.
	if len(head) > 0 {
		head = slices.Insert(head, 0, any(newHTTPSFilterRule()))
	}

	dns["rules"] = slices.Concat(hostsRules, head, userRules, tail)

	return nil
}

// newGroupDNSRules возвращает системные DNS-правила группы (пустые, если у группы нет DNS-сервера).
func newGroupDNSRules(group Group) *groupDNSRules {
	if group.DNSServer == "" {
		return &groupDNSRules{
			head:   []any{},
			legacy: []any{},
		}
	}

	return &groupDNSRules{
		head: []any{
			map[string]any{
				"rule_set": getRuleSetTagForGroup(group.Name),
				"server":   group.DNSServer,
			},
		},
		legacy: []any{},
	}
}

// newHTTPSFilterRule возвращает правило, отвечающее на запросы HTTPS-записей пустым ответом.
func newHTTPSFilterRule() map[string]any {
	return map[string]any{
		"action":     "predefined",
		"query_type": []any{"HTTPS"},
		"rcode":      "NOERROR",
	}
}

// isHTTPSFilterRule проверяет, что правило — системный фильтр HTTPS-записей.
func isHTTPSFilterRule(rule map[string]any) bool {
	if len(rule) != 3 {
		return false
	}

	action, _ := extractFiledFromMapAny(rule, "action")
	rcode, _ := extractFiledFromMapAny(rule, "rcode")
	queryTypes, _ := rule["query_type"].([]any)

	return action == "predefined" && rcode == "NOERROR" && slices.Equal(queryTypes, []any{"HTTPS"})
}

// parseSystemDNSRule определяет, является ли правило системным DNS-правилом группы.
// Возвращает имя группы и признак правила из начала списка (домены группы).
func parseSystemDNSRule(rule map[string]any) (string, bool, bool) {
	action, _ := extractFiledFromMapAny(rule, "action")

	switch action {
	// Правило доменов группы: только rule_set группы и сервер, action может быть опущен.
	case "", "route":
		tags := extractRuleSetTags(rule)

		if len(tags) != 1 || !isGroupRuleSetTag(tags[0]) {
			return "", false, false
		}

		if _, ok := extractFiledFromMapAny(rule, "server"); !ok {
			return "", false, false
		}

		for key := range rule {
			if key != "rule_set" && key != "server" && key != "action" {
				return "", false, false
			}
		}

		return strings.TrimPrefix(tags[0], ruleSetTagPrefix+"-"), true, true

	case "evaluate":
		tag, _ := extractFiledFromMapAny(rule, "tag")

		if groupName, ok := parseDNSEvaluateTag(tag); ok {
			return groupName, false, true
		}

	case "respond":
		tag, _ := extractFiledFromMapAny(rule, "match_response")

		if groupName, ok := parseDNSEvaluateTag(tag); ok {
			return groupName, false, true
		}
	}

	return "", false, false
}

// isGroupRuleSetTag проверяет, что тег — доменный rule-set группы.
func isGroupRuleSetTag(tag string) bool {
	if !strings.HasPrefix(tag, ruleSetTagPrefix+"-") || tag == BypassRuleSetTag {
		return false
	}

	return !strings.Contains(tag, "@")
}

// parseDNSEvaluateTag возвращает имя группы из тега evaluate-ответа.
func parseDNSEvaluateTag(tag string) (string, bool) {
	if !strings.HasPrefix(tag, ruleSetTagPrefix+"-") || !strings.HasSuffix(tag, dnsEvaluateTagSuffix) {
		return "", false
	}

	groupName := strings.TrimSuffix(strings.TrimPrefix(tag, ruleSetTagPrefix+"-"), dnsEvaluateTagSuffix)

	return groupName, groupName != ""
}

// GetGroupDNSServers возвращает DNS-серверы групп из системных DNS-правил конфига.
func GetGroupDNSServers(config map[string]any) map[string]string {
	servers := map[string]string{}

	dns, _ := config["dns"].(map[string]any)
	rules, _ := dns["rules"].([]any)

	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}

		groupName, isHead, ok := parseSystemDNSRule(rule)
		if !ok || !isHead {
			continue
		}

		server, _ := extractFiledFromMapAny(rule, "server")
		servers[groupName] = server
	}

	return servers
}

// hasHTTPSFilterRule проверяет, что в конфиге есть фильтр HTTPS-записей.
func hasHTTPSFilterRule(config map[string]any) bool {
	dns, _ := config["dns"].(map[string]any)
	rules, _ := dns["rules"].([]any)

	return slices.ContainsFunc(rules, func(item any) bool {
		rule, ok := item.(map[string]any)

		return ok && isHTTPSFilterRule(rule)
	})
}

// hasLegacyGroupDNSRules проверяет, остались ли в конфиге правила evaluate/respond группы от версии 0.4.0.
func hasLegacyGroupDNSRules(config map[string]any, groupName string) bool {
	dns, _ := config["dns"].(map[string]any)
	rules, _ := dns["rules"].([]any)

	return slices.ContainsFunc(rules, func(item any) bool {
		rule, ok := item.(map[string]any)
		if !ok {
			return false
		}

		name, isHead, ok := parseSystemDNSRule(rule)

		return ok && !isHead && name == groupName
	})
}
