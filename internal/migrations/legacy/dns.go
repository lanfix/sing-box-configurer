// Package legacy содержит разбор конфига sing-box в формате, который конфигуратор записывал до того,
// как точкой правды стал app.json. Используется только миграциями данных.
package legacy

import (
	"slices"
	"strings"
)

const (
	// RuleSetTagPrefix — префикс тегов системных rule-set-ов.
	RuleSetTagPrefix = "configurer"

	// SelectorTagPrefix — префикс тегов системных selector-ов групп.
	SelectorTagPrefix = "select"

	// ipRuleSetTagSuffix — суффикс тега rule-set-а с IP/CIDR группы.
	ipRuleSetTagSuffix = "@ip"

	// dnsEvaluateTagSuffix — суффикс тега evaluate-ответа группы, такие правила создавала версия 0.4.0.
	dnsEvaluateTagSuffix = "@dns"
)

// ParseSystemDNSRule определяет, является ли правило системным DNS-правилом группы.
// Возвращает имя группы и признак правила из начала списка (домены группы).
func ParseSystemDNSRule(rule map[string]any) (string, bool, bool) {
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

		return strings.TrimPrefix(tags[0], RuleSetTagPrefix+"-"), true, true

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

// IsHTTPSFilterRule проверяет, что правило — системный фильтр HTTPS-записей.
func IsHTTPSFilterRule(rule map[string]any) bool {
	if len(rule) != 3 {
		return false
	}

	action, _ := extractFiledFromMapAny(rule, "action")
	rcode, _ := extractFiledFromMapAny(rule, "rcode")
	queryTypes, _ := rule["query_type"].([]any)

	return action == "predefined" && rcode == "NOERROR" && slices.Equal(queryTypes, []any{"HTTPS"})
}

// isGroupRuleSetTag проверяет, что тег — доменный rule-set группы.
func isGroupRuleSetTag(tag string) bool {
	if !strings.HasPrefix(tag, RuleSetTagPrefix+"-") || tag == BypassRuleSetTag {
		return false
	}

	return !strings.Contains(tag, "@")
}

// parseDNSEvaluateTag возвращает имя группы из тега evaluate-ответа.
func parseDNSEvaluateTag(tag string) (string, bool) {
	if !strings.HasPrefix(tag, RuleSetTagPrefix+"-") || !strings.HasSuffix(tag, dnsEvaluateTagSuffix) {
		return "", false
	}

	groupName := strings.TrimSuffix(strings.TrimPrefix(tag, RuleSetTagPrefix+"-"), dnsEvaluateTagSuffix)

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

		groupName, isHead, ok := ParseSystemDNSRule(rule)
		if !ok || !isHead {
			continue
		}

		server, _ := extractFiledFromMapAny(rule, "server")
		servers[groupName] = server
	}

	return servers
}

// extractRuleSetTags возвращает теги из поля rule_set правила (строка или список строк).
func extractRuleSetTags(rule map[string]any) []string {
	switch value := rule["rule_set"].(type) {
	case string:
		return []string{value}

	case []any:
		tags := make([]string, 0, len(value))

		for _, item := range value {
			if tag, ok := item.(string); ok {
				tags = append(tags, tag)
			}
		}

		return tags

	default:
		return nil
	}
}

// extractFiledFromMapAny возвращает строковое значение поля key.
func extractFiledFromMapAny(data map[string]any, key string) (string, bool) {
	untyped, ok := data[key]
	if !ok {
		return "", false
	}

	val, ok := untyped.(string)
	if !ok {
		return "", false
	}

	return val, true
}
