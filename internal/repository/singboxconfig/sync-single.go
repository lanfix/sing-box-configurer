package singboxconfig

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	// Префикс, с которого начинаются имена тегов системных rule-set-ов.
	ruleSetTagPrefix = "configurer"

	// Префикс, с которого начинается имя тегов системных selector-ов.
	selectorTagPrefix = "select"
)

// SyncSingleGroup синхронизирует одну группу во временный конфиг sing-box.
func (p *Provider) SyncSingleGroup(group Group, allGroups []Group) error {
	configData, err := p.GetTempOrActualConfig()
	if err != nil {
		return fmt.Errorf("cannot get config: %w", err)
	}

	// Удаляем комментарии перед парсингом.
	cleanedConfigData := removeComments(configData)

	var config map[string]any

	if err = json.Unmarshal(cleanedConfigData, &config); err != nil {
		return fmt.Errorf("cannot unmarshal json: %w", err)
	}

	if err = syncGroups(config, []Group{group}, allGroups); err != nil {
		return fmt.Errorf("cannot sync group: %w", err)
	}

	// Сохраняем конфиг обратно.
	configData, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err = p.SaveTempConfig(configData); err != nil {
		return fmt.Errorf("cannot save temp config: %w", err)
	}

	return nil
}

// syncGroups приводит системные rule-set-ы, правила и selector-ы групп targets к желаемому состоянию.
// Системные записи остальных групп из allGroups сохраняются, пользовательские записи не трогаются.
// Функция идемпотентна: повторный вызов не создает дубликатов.
func syncGroups(config map[string]any, targets []Group, allGroups []Group) error {
	var route map[string]any

	if routeUntyped, ok := config["route"]; ok {
		route, ok = routeUntyped.(map[string]any)
		if !ok {
			return fmt.Errorf(".route should be map[string]any")
		}
	} else {
		route = map[string]any{}
	}

	var ruleSets []any

	if ruleSetsUntyped, ok := route["rule_set"]; ok {
		ruleSets, ok = ruleSetsUntyped.([]any)
		if !ok {
			return fmt.Errorf(".route.rule_set should be []any")
		}
	} else {
		ruleSets = []any{}
	}

	route["rule_set"] = getDesiredRuleSetList(ruleSets, targets, allGroups)

	var rules []any

	if rulesUntyped, ok := route["rules"]; ok {
		rules, ok = rulesUntyped.([]any)
		if !ok {
			return fmt.Errorf(".rules should be []any")
		}
	} else {
		rules = []any{}
	}

	route["rules"] = getDesiredRulesList(rules, targets, allGroups)
	config["route"] = route

	var outbounds []any

	if outboundsUntyped, ok := config["outbounds"]; ok {
		outbounds, ok = outboundsUntyped.([]any)
		if !ok {
			return fmt.Errorf(".outbounds should be []any")
		}
	} else {
		outbounds = []any{}
	}

	var endpoints []any

	if endpointsUntyped, ok := config["endpoints"]; ok {
		endpoints, ok = endpointsUntyped.([]any)
		if !ok {
			return fmt.Errorf(".endpoints should be []any")
		}
	} else {
		endpoints = []any{}
	}

	config["outbounds"] = getDesiredOutboundsList(outbounds, endpoints, targets, allGroups)

	return nil
}

// getDesiredRuleSetList возвращает список rule-set-ов с актуальными системными rule-set-ами групп targets.
func getDesiredRuleSetList(actualRuleSetList []any, targets []Group, allGroups []Group) []any {
	desiredRuleSetList := make([]any, 0)
	systemRuleSetMap := map[string]any{}

	for i := range actualRuleSetList {
		rsMap, ok := actualRuleSetList[i].(map[string]any)
		if !ok {
			// Не системные rule-set-ы добавляем как есть.
			desiredRuleSetList = append(desiredRuleSetList, actualRuleSetList[i])

			continue
		}

		tag, ok := rsMap["tag"].(string)
		if !ok {
			// Не системные rule-set-ы добавляем как есть.
			desiredRuleSetList = append(desiredRuleSetList, rsMap)

			continue
		}

		if !strings.HasPrefix(tag, ruleSetTagPrefix) {
			// Не системные rule-set-ы добавляем как есть.
			desiredRuleSetList = append(desiredRuleSetList, rsMap)

			continue
		}

		// Системные rule-set-ы откладываем в отдельную map-у, чтобы обработать позже.
		systemRuleSetMap[tag] = actualRuleSetList[i]
	}

	// Обновляем данные rule-set-ов.
	for _, group := range targets {
		desiredRuleSetTag := getRuleSetTagForGroup(group.Name)

		systemRuleSetMap[desiredRuleSetTag] = map[string]any{
			"format": "source",
			"http_client": map[string]any{
				"tag": "default_http_client",
			},
			"tag":             desiredRuleSetTag,
			"type":            "remote",
			"update_interval": "30s",
			"url":             fmt.Sprintf("http://127.0.0.1:8080/api/ruleset/group?group=%s", group.Name), // TODO: Вынести хост в конфиг.
		}
	}

	// Выстраиваем системные rule-set-ы в порядке из allGroups.
	for i := range allGroups {
		tag := getRuleSetTagForGroup(allGroups[i].Name)

		ruleSet, ok := systemRuleSetMap[tag]
		if !ok {
			continue
		}

		desiredRuleSetList = append(desiredRuleSetList, ruleSet)
	}

	return desiredRuleSetList
}

// getDesiredRulesList возвращает список правил с актуальными системными правилами групп targets в конце.
func getDesiredRulesList(actualRulesList []any, targets []Group, allGroups []Group) []any {
	desiredRules := make([]any, 0)
	systemRulesMap := map[string]any{}

	for i := range actualRulesList {
		ruleMap, ok := actualRulesList[i].(map[string]any)
		if !ok {
			desiredRules = append(desiredRules, actualRulesList[i])

			continue
		}

		outboundTag, ok := extractFiledFromMapAny(ruleMap, "outbound")
		if !ok {
			desiredRules = append(desiredRules, ruleMap)

			continue
		}

		ruleSetTag, ok := extractFiledFromMapAny(ruleMap, "rule_set")
		if !ok {
			desiredRules = append(desiredRules, ruleMap)

			continue
		}

		if !strings.HasPrefix(outboundTag, selectorTagPrefix) || !strings.HasPrefix(ruleSetTag, ruleSetTagPrefix) {
			// Если нет специальных системных префиксов в именах тегов, то это обычное пользовательское правило.
			desiredRules = append(desiredRules, ruleMap)

			continue
		}

		systemRulesMap[ruleSetTag] = ruleMap
	}

	for _, group := range targets {
		desiredRuleSetTag := getRuleSetTagForGroup(group.Name)

		systemRulesMap[desiredRuleSetTag] = map[string]any{
			"outbound": getSelectorTagForGroup(group.Name),
			"rule_set": desiredRuleSetTag,
		}
	}

	for i := range allGroups {
		tag := getRuleSetTagForGroup(allGroups[i].Name)

		rule, ok := systemRulesMap[tag]
		if !ok {
			continue
		}

		desiredRules = append(desiredRules, rule)
	}

	return desiredRules
}

// getDesiredOutboundsList возвращает список outbounds с актуальными selector-ами групп targets в конце.
func getDesiredOutboundsList(actualOutboundsList, actualEndpointsList []any, targets []Group, allGroups []Group) []any {
	desiredOutbounds := make([]any, 0)
	systemOutboundsMap := map[string]any{}
	outboundTags := make([]string, 0)

	for i := range actualOutboundsList {
		outboundMap, ok := actualOutboundsList[i].(map[string]any)
		if !ok {
			desiredOutbounds = append(desiredOutbounds, actualOutboundsList[i])

			continue
		}

		tag, ok := extractFiledFromMapAny(outboundMap, "tag")
		if !ok {
			desiredOutbounds = append(desiredOutbounds, outboundMap)

			continue
		}

		if !strings.HasPrefix(tag, selectorTagPrefix) {
			desiredOutbounds = append(desiredOutbounds, outboundMap)

			// Не добавляем в список теги от самих себя (от селекторов).
			outboundTags = append(outboundTags, tag)

			continue
		}

		systemOutboundsMap[tag] = outboundMap
	}

	for i := range actualEndpointsList {
		endpointMap, ok := actualEndpointsList[i].(map[string]any)
		if !ok {
			continue
		}

		tag, ok := extractFiledFromMapAny(endpointMap, "tag")
		if !ok {
			continue
		}

		outboundTags = append(outboundTags, tag)
	}

	for _, group := range targets {
		desiredSelectorTag := getSelectorTagForGroup(group.Name)

		defaultOutbound := group.DefaultOutbound

		// Если нет дефолтного outbound, определенного в группе, блокируем трафик.
		if !slices.Contains(outboundTags, defaultOutbound) {
			defaultOutbound = "block" // TODO: Сделать системный outbound block.
		}

		systemOutboundsMap[desiredSelectorTag] = map[string]any{
			"default":                     defaultOutbound,
			"interrupt_exist_connections": true,
			"outbounds":                   slices.Clone(outboundTags),
			"tag":                         desiredSelectorTag,
			"type":                        "selector",
		}
	}

	for i := range allGroups {
		tag := getSelectorTagForGroup(allGroups[i].Name)

		outbound, ok := systemOutboundsMap[tag]
		if !ok {
			continue
		}

		desiredOutbounds = append(desiredOutbounds, outbound)
	}

	return desiredOutbounds
}

func getRuleSetTagForGroup(groupName string) string {
	return fmt.Sprintf("%s-%s", ruleSetTagPrefix, groupName)
}

func getSelectorTagForGroup(groupName string) string {
	return fmt.Sprintf("%s-%s", selectorTagPrefix, groupName)
}

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
