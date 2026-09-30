package topology

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/render"
)

// parsedRule — правило маршрутизации или DNS рабочего конфига вместе со строкой карты.
type parsedRule struct {
	// raw — правило как есть, nil для строки final.
	raw    map[string]any
	action string
	row    Row

	// tokens — фрагменты описания правила в Clash API: по ним соединение сопоставляется со строкой.
	tokens []string
}

// actionKeys — поля правила, которые относятся к действию, а не к условиям.
var actionKeys = map[string]bool{
	"action":                       true,
	"outbound":                     true,
	"server":                       true,
	"override_address":             true,
	"override_port":                true,
	"network_strategy":             true,
	"network_type":                 true,
	"fallback_network_type":        true,
	"fallback_delay":               true,
	"udp_disable_domain_unmapping": true,
	"udp_connect":                  true,
	"udp_timeout":                  true,
	"tls_fragment":                 true,
	"tls_fragment_fallback_delay":  true,
	"tls_record_fragment":          true,
	"timeout":                      true,
	"sniffer":                      true,
	"method":                       true,
	"no_drop":                      true,
	"strategy":                     true,
	"rcode":                        true,
	"answer":                       true,
	"ns":                           true,
	"extra":                        true,
	"disable_cache":                true,
	"rewrite_ttl":                  true,
	"client_subnet":                true,
	"invert":                       true,
	"type":                         true,
	"mode":                         true,
	"rules":                        true,
}

// conditionKeys возвращает отсортированные поля условий правила.
func conditionKeys(rule map[string]any) []string {
	keys := make([]string, 0, len(rule))

	for key := range rule {
		if !actionKeys[key] {
			keys = append(keys, key)
		}
	}

	slices.Sort(keys)

	return keys
}

// parseRouteRules разбирает route.rules и добавляет строку final.
func parseRouteRules(config map[string]any, groups map[string]GroupInfo) []parsedRule {
	route, _ := config["route"].(map[string]any)
	list := objects(route["rules"])
	result := make([]parsedRule, 0, len(list)+1)

	for i, raw := range list {
		action := jsonmap.String(raw, "action")
		if action == "" {
			action = "route"
		}

		label, detail, groupNames := describeRule(raw, groups)

		row := Row{
			ID:     fmt.Sprintf("r%d", i),
			Index:  i,
			Label:  label,
			Detail: detail,
			Action: action,
			Target: "",
			Groups: groupNames,
		}

		switch action {
		case "route":
			if tag := jsonmap.String(raw, "outbound"); tag != "" {
				row.Target = OutboundID(tag)
			}

		case "reject":
			row.Target = RejectID

		case "bypass":
			row.Target = BypassID

			if tag := jsonmap.String(raw, "outbound"); tag != "" {
				row.Target = OutboundID(tag)
			}

		case "hijack-dns":
			row.Target = DNSRouterID
		}

		result = append(result, parsedRule{
			raw:    raw,
			action: action,
			row:    row,
			tokens: fingerprint(raw),
		})
	}

	final := jsonmap.String(route, "final")

	if final == "" {
		if first := objects(config["outbounds"]); len(first) > 0 {
			final = jsonmap.String(first[0], "tag")
		}
	}

	finalRow := Row{
		ID:     FinalRow,
		Index:  -1,
		Label:  "Остальной трафик",
		Detail: "final",
		Action: "route",
		Target: "",
		Groups: nil,
	}

	if final != "" {
		finalRow.Target = OutboundID(final)
	}

	return append(result, parsedRule{
		raw:    nil,
		action: "route",
		row:    finalRow,
		tokens: nil,
	})
}

// parseDNSRules разбирает dns.rules и добавляет строку final.
func parseDNSRules(dns map[string]any, groups map[string]GroupInfo) []parsedRule {
	list := objects(dns["rules"])
	result := make([]parsedRule, 0, len(list)+1)

	for i, raw := range list {
		action := jsonmap.String(raw, "action")
		if action == "" {
			action = "route"
		}

		label, detail, groupNames := describeRule(raw, groups)
		server := jsonmap.String(raw, "server")

		if server == render.HostsServerTag {
			label = "DNS-записи"
			detail = plural(len(jsonmap.Strings(raw, "domain")), "домен", "домена", "доменов")
		}

		row := Row{
			ID:     fmt.Sprintf("d%d", i),
			Index:  i,
			Label:  label,
			Detail: detail,
			Action: action,
			Target: "",
			Groups: groupNames,
		}

		if action == "route" && server != "" {
			row.Target = DNSServerID(server)
		}

		result = append(result, parsedRule{
			raw:    raw,
			action: action,
			row:    row,
			tokens: nil,
		})
	}

	final := jsonmap.String(dns, "final")

	if final == "" {
		if servers := objects(dns["servers"]); len(servers) > 0 {
			final = jsonmap.String(servers[0], "tag")
		}
	}

	finalRow := Row{
		ID:     DNSFinalRow,
		Index:  -1,
		Label:  "Остальные запросы",
		Detail: "final",
		Action: "route",
		Target: "",
		Groups: nil,
	}

	if final != "" {
		finalRow.Target = DNSServerID(final)
	}

	return append(result, parsedRule{
		raw:    nil,
		action: "route",
		row:    finalRow,
		tokens: nil,
	})
}

// describeRule возвращает подпись правила, пояснение и группы конфигуратора, на rule-set-ы которых
// оно ссылается.
func describeRule(rule map[string]any, groups map[string]GroupInfo) (string, string, []string) {
	if jsonmap.String(rule, "type") == "logical" {
		return fmt.Sprintf("Логическое правило (%s)", jsonmap.String(rule, "mode")), "", nil
	}

	keys := conditionKeys(rule)
	inbounds := jsonmap.Strings(rule, "inbound")
	details := make([]string, 0, 2)

	var (
		label      string
		groupNames []string
	)

	switch {
	case slices.Contains(keys, "rule_set"):
		var names []string

		names, groupNames = ruleSetNames(jsonmap.Strings(rule, "rule_set"))
		label = strings.Join(names, ", ")

		if stats := groupsDetail(groupNames, groups); stats != "" {
			details = append(details, stats)
		}

	case jsonmap.Bool(rule, "ip_is_private"):
		label = "Приватные IP"

	case slices.Contains(jsonmap.Strings(rule, "protocol"), "dns") || slices.Contains(ints(rule["port"]), 53):
		label = "DNS-запросы"

	case slices.Contains(keys, "query_type"):
		label = "Запросы " + strings.Join(jsonmap.Strings(rule, "query_type"), ", ")

	case len(keys) == 0:
		label = "Все соединения"

	case len(keys) == 1 && keys[0] == "inbound":
		label = "Inbound " + strings.Join(inbounds, ", ")
		inbounds = nil

	default:
		label = conditionSummary(rule, keys[0])

		if len(keys) > 1 {
			details = append(details, fmt.Sprintf("и еще условий: %d", len(keys)-1))
		}
	}

	if len(inbounds) > 0 {
		details = append(details, "только "+strings.Join(inbounds, ", "))
	}

	if jsonmap.Bool(rule, "invert") {
		label = "не " + label
	}

	return label, strings.Join(details, " · "), groupNames
}

// ruleSetNames возвращает подписи rule-set-ов: для наборов конфигуратора — имя группы (один раз
// на домены и IP), для чужих — тег. Вторым значением возвращаются имена групп.
func ruleSetNames(tags []string) ([]string, []string) {
	names := make([]string, 0, len(tags))
	groupNames := make([]string, 0, len(tags))

	for _, tag := range tags {
		name := tag

		if group, _, ok := render.ParseRuleSetTag(tag); ok {
			name = group

			if !slices.Contains(groupNames, group) {
				groupNames = append(groupNames, group)
			}
		}

		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return names, groupNames
}

// groupsDetail возвращает размеры наборов групп groupNames.
func groupsDetail(groupNames []string, groups map[string]GroupInfo) string {
	var domains, ips, sources int

	found := false

	for _, name := range groupNames {
		group, ok := groups[name]
		if !ok {
			continue
		}

		found = true
		domains += group.Stats.Domains + group.Stats.Suffixes
		ips += group.Stats.IPs
		sources += group.Stats.Sources
	}

	if !found {
		return ""
	}

	parts := []string{
		plural(domains, "домен", "домена", "доменов"),
		fmt.Sprintf("%d IP", ips),
	}

	if sources > 0 {
		parts = append(parts, plural(sources, "источник", "источника", "источников"))
	}

	return strings.Join(parts, " · ")
}

// conditionSummary возвращает краткую запись условия key: имя и первые значения.
func conditionSummary(rule map[string]any, key string) string {
	values := jsonmap.Strings(rule, key)

	if values == nil {
		return fmt.Sprintf("%s: %v", key, rule[key])
	}

	if len(values) > 2 {
		return fmt.Sprintf("%s: %s … (+%d)", key, strings.Join(values[:2], ", "), len(values)-2)
	}

	return key + ": " + strings.Join(values, ", ")
}

// fingerprint возвращает фрагменты, которые sing-box выводит в описании правила в Clash API.
func fingerprint(rule map[string]any) []string {
	tokens := make([]string, 0, 4)

	for _, key := range conditionKeys(rule) {
		switch key {
		case "rule_set", "inbound":
			tokens = append(tokens, jsonmap.Strings(rule, key)...)

		default:
			tokens = append(tokens, key+"=")
		}
	}

	return tokens
}

// ints возвращает число или список чисел из JSON-значения.
func ints(value any) []int {
	switch typed := value.(type) {
	case float64:
		return []int{int(typed)}

	case int:
		return []int{typed}

	case []any:
		result := make([]int, 0, len(typed))

		for _, item := range typed {
			if number, ok := item.(float64); ok {
				result = append(result, int(number))
			}
		}

		return result

	default:
		return nil
	}
}

// plural возвращает число с подходящей формой слова: один, два, пять.
func plural(count int, one, few, many string) string {
	form := many
	mod10 := count % 10
	mod100 := count % 100

	switch {
	case mod10 == 1 && mod100 != 11:
		form = one

	case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
		form = few
	}

	return fmt.Sprintf("%d %s", count, form)
}
