package legacy

import (
	"fmt"
	"slices"
)

const (
	// BypassRuleSetTag — тег системного rule-set-а с правилами, исключенными из туннелирования.
	BypassRuleSetTag = RuleSetTagPrefix + "-bypass"

	// bypassRuleSetURL — адрес, по которому sing-box забирал правила исключений у конфигуратора
	// на момент миграции 2. Значение зафиксировано: миграции не меняются после релиза.
	bypassRuleSetURL = "http://127.0.0.1:8080/api/ruleset/bypass"
)

// EnsureBypass приводит в конфиге sing-box системные записи исключений из туннелирования
// к желаемому состоянию. Функция идемпотентна.
//
// Исключения работают только для tun-inbound-ов с auto_redirect:
//   - IP/CIDR из rule-set-а попадают в route_exclude_address_set и отсекаются в nftables;
//   - домены и суффиксы матчатся правилом с action bypass в pre-match. Правило ставится первым,
//     т.к. pre-match для TCP останавливается на sniff, а домен берется из dns.reverse_mapping.
//
// Если подходящих tun-inbound-ов нет, системные записи исключений удаляются.
func EnsureBypass(config map[string]any) error {
	route, err := getOrCreateMap(config, "route")
	if err != nil {
		return err
	}

	ruleSets, err := getSlice(route, "rule_set")
	if err != nil {
		return fmt.Errorf(".route: %w", err)
	}

	rules, err := getSlice(route, "rules")
	if err != nil {
		return fmt.Errorf(".route: %w", err)
	}

	inbounds, err := getSlice(config, "inbounds")
	if err != nil {
		return err
	}

	// Убираем прежние системные записи, чтобы добавить их заново в нужном виде и месте.
	ruleSets = slices.DeleteFunc(ruleSets, func(item any) bool {
		ruleSetMap, ok := item.(map[string]any)
		if !ok {
			return false
		}

		tag, _ := extractFiledFromMapAny(ruleSetMap, "tag")

		return tag == BypassRuleSetTag
	})

	rules = slices.DeleteFunc(rules, isBypassRule)

	autoRedirectInbounds := make([]map[string]any, 0, 1)

	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if inboundType, _ := extractFiledFromMapAny(inbound, "type"); inboundType != "tun" {
			continue
		}

		if err = removeExcludeAddressSet(inbound); err != nil {
			return err
		}

		if autoRedirect, _ := inbound["auto_redirect"].(bool); autoRedirect {
			autoRedirectInbounds = append(autoRedirectInbounds, inbound)
		}
	}

	if len(autoRedirectInbounds) > 0 {
		ruleSets = append(ruleSets, map[string]any{
			"format": "source",
			"http_client": map[string]any{
				"tag": "default_http_client",
			},
			"tag":             BypassRuleSetTag,
			"type":            "remote",
			"update_interval": "30s",
			"url":             bypassRuleSetURL,
		})

		rules = slices.Insert(rules, 0, any(map[string]any{
			"action":   "bypass",
			"rule_set": BypassRuleSetTag,
		}))

		for _, inbound := range autoRedirectInbounds {
			excludeSets, _ := inbound["route_exclude_address_set"].([]any)
			inbound["route_exclude_address_set"] = append(excludeSets, BypassRuleSetTag)
		}

		dns, err := getOrCreateMap(config, "dns")
		if err != nil {
			return err
		}

		// Без обратного маппинга в pre-match известен только IP, и правила по доменам не сработают.
		dns["reverse_mapping"] = true
	}

	route["rule_set"] = ruleSets
	route["rules"] = rules

	return nil
}

// isBypassRule проверяет, что правило — системное правило исключений из туннелирования.
func isBypassRule(item any) bool {
	ruleMap, ok := item.(map[string]any)
	if !ok {
		return false
	}

	action, _ := extractFiledFromMapAny(ruleMap, "action")
	ruleSet, _ := extractFiledFromMapAny(ruleMap, "rule_set")

	return action == "bypass" && ruleSet == BypassRuleSetTag
}

// removeExcludeAddressSet удаляет системный rule-set из route_exclude_address_set inbound-а.
func removeExcludeAddressSet(inbound map[string]any) error {
	untyped, ok := inbound["route_exclude_address_set"]
	if !ok {
		return nil
	}

	var excludeSets []any

	switch value := untyped.(type) {
	// sing-box допускает одиночное значение вместо списка.
	case string:
		excludeSets = []any{value}

	case []any:
		excludeSets = value

	default:
		return fmt.Errorf("route_exclude_address_set should be string or []any")
	}

	excludeSets = slices.DeleteFunc(excludeSets, func(item any) bool {
		return item == BypassRuleSetTag
	})

	if len(excludeSets) == 0 {
		delete(inbound, "route_exclude_address_set")

		return nil
	}

	inbound["route_exclude_address_set"] = excludeSets

	return nil
}

// getOrCreateMap возвращает вложенный объект по ключу key, создавая его при отсутствии.
func getOrCreateMap(parent map[string]any, key string) (map[string]any, error) {
	untyped, ok := parent[key]
	if !ok {
		child := map[string]any{}
		parent[key] = child

		return child, nil
	}

	child, ok := untyped.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(".%s should be map[string]any", key)
	}

	return child, nil
}

// getSlice возвращает вложенный список по ключу key (пустой, если ключа нет).
func getSlice(parent map[string]any, key string) ([]any, error) {
	untyped, ok := parent[key]
	if !ok {
		return []any{}, nil
	}

	list, ok := untyped.([]any)
	if !ok {
		return nil, fmt.Errorf(".%s should be []any", key)
	}

	return list, nil
}
