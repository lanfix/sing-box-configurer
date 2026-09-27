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

	// dnsEvaluateTagSuffix — суффикс тега evaluate-ответа группы в DNS-правилах.
	dnsEvaluateTagSuffix = "@dns"
)

// dnsAddressQueryTypes — типы запросов, ответы на которые сверяются с IP/CIDR групп.
var dnsAddressQueryTypes = []any{"A", "AAAA"}

// groupDNSRules — системные DNS-правила одной группы.
type groupDNSRules struct {
	// head — правила в начале dns.rules (домены группы).
	head []any

	// tail — правила в конце dns.rules (сверка ответа с IP/CIDR группы).
	tail []any
}

// syncDNSRules приводит системные DNS-правила групп targets к желаемому состоянию. Системные правила
// остальных групп из allGroups сохраняются как есть, пользовательские правила не трогаются.
//
// Для группы с DNS-сервером создаются:
//   - в начале dns.rules: запросы к доменам группы уходят на сервер группы;
//   - в конце dns.rules: A/AAAA-запросы вычисляются через сервер группы (evaluate), и если ответ
//     попадает в IP/CIDR группы, возвращается он (respond). Остальные запросы идут дальше, к dns.final.
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
	systemRules := map[string]*groupDNSRules{}

	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			userRules = append(userRules, item)

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
				head: []any{},
				tail: []any{},
			}
			systemRules[groupName] = groupRules
		}

		if isHead {
			groupRules.head = append(groupRules.head, rule)
		} else {
			groupRules.tail = append(groupRules.tail, rule)
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
		tail = append(tail, groupRules.tail...)
	}

	// Фильтр системный: созданный ранее конфигуратором не отличить от добавленного вручную.
	if len(head) > 0 {
		head = slices.Insert(head, 0, any(newHTTPSFilterRule()))
	}

	dns["rules"] = slices.Concat(head, userRules, tail)

	return nil
}

// newGroupDNSRules возвращает системные DNS-правила группы (пустые, если у группы нет DNS-сервера).
func newGroupDNSRules(group Group) *groupDNSRules {
	if group.DNSServer == "" {
		return &groupDNSRules{
			head: []any{},
			tail: []any{},
		}
	}

	evaluateTag := getDNSEvaluateTagForGroup(group.Name)

	return &groupDNSRules{
		head: []any{
			map[string]any{
				"rule_set": getRuleSetTagForGroup(group.Name),
				"server":   group.DNSServer,
			},
		},
		tail: []any{
			map[string]any{
				"action":     "evaluate",
				"query_type": slices.Clone(dnsAddressQueryTypes),
				"server":     group.DNSServer,
				"tag":        evaluateTag,
			},
			map[string]any{
				"action":         "respond",
				"match_response": evaluateTag,
				"query_type":     slices.Clone(dnsAddressQueryTypes),
				"rule_set":       getIPRuleSetTagForGroup(group.Name),
			},
		},
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

// getDNSEvaluateTagForGroup возвращает тег evaluate-ответа группы.
func getDNSEvaluateTagForGroup(groupName string) string {
	return getRuleSetTagForGroup(groupName) + dnsEvaluateTagSuffix
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

// hasGroupDNSRules проверяет, что в конфиге есть полный набор системных DNS-правил группы.
func hasGroupDNSRules(config map[string]any, group Group) bool {
	dns, _ := config["dns"].(map[string]any)
	rules, _ := dns["rules"].([]any)

	var hasEvaluate, hasRespond bool

	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}

		groupName, isHead, ok := parseSystemDNSRule(rule)
		if !ok || isHead || groupName != group.Name {
			continue
		}

		action, _ := extractFiledFromMapAny(rule, "action")
		server, _ := extractFiledFromMapAny(rule, "server")

		if action == "evaluate" && server == group.DNSServer {
			hasEvaluate = true
		}

		if action == "respond" && slices.Equal(extractRuleSetTags(rule), []string{getIPRuleSetTagForGroup(group.Name)}) {
			hasRespond = true
		}
	}

	return hasEvaluate && hasRespond
}
