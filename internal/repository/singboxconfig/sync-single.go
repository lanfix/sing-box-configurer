package singboxconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SyncSingleGroupToConfig синхронизирует одну группу в конфиг sing-box.
func (p *Provider) SyncSingleGroupToConfig(configPath string, group Group, allGroups []Group) error {
	// Читаем конфиг sing-box.
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cannot read config: %w", err)
	}

	// Удаляем комментарии перед парсингом.
	cleanedConfigData := removeComments(configData)

	var config map[string]interface{}

	if err := json.Unmarshal(cleanedConfigData, &config); err != nil {
		return fmt.Errorf("cannot parse config: %w", err)
	}

	// Получаем секцию route.
	route, ok := config["route"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("route section not found in config")
	}

	// Получаем rule_set.
	var ruleSets []interface{}

	if rs, ok := route["rule_set"].([]interface{}); ok {
		ruleSets = rs
	} else {
		ruleSets = []interface{}{}
	}

	// Получаем rules.
	var rules []interface{}

	if r, ok := route["rules"].([]interface{}); ok {
		rules = r
	} else {
		rules = []interface{}{}
	}

	// Обновляем или добавляем rule_set для данной группы.
	ruleSetTag := "configurer-" + group.Name
	foundRuleSet := false

	for i, rs := range ruleSets {
		rsMap, ok := rs.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := rsMap["tag"].(string)
		if !ok {
			continue
		}

		if tag == ruleSetTag {
			// Обновляем существующий rule_set.
			rsMap["format"] = "source"
			rsMap["http_client"] = map[string]interface{}{
				"tag": "default_http_client",
			}
			rsMap["type"] = "remote"
			rsMap["update_interval"] = "30s"
			rsMap["url"] = fmt.Sprintf("http://127.0.0.1:8080/api/ruleset/group?group=%s", group.Name)
			ruleSets[i] = rsMap
			foundRuleSet = true

			break
		}
	}

	if !foundRuleSet {
		// Добавляем новый rule_set в конец (сохраняя порядок).
		ruleSet := map[string]interface{}{
			"format": "source",
			"http_client": map[string]interface{}{
				"tag": "default_http_client",
			},
			"tag":             ruleSetTag,
			"type":            "remote",
			"update_interval": "30s",
			"url":             fmt.Sprintf("http://127.0.0.1:8080/api/ruleset/group?group=%s", group.Name),
		}

		ruleSets = append(ruleSets, ruleSet)
	}

	route["rule_set"] = ruleSets

	// Обрабатываем rules: удаляем все правила групп и добавляем их заново в правильном порядке.
	var userRules []interface{}
	var serviceRules []interface{}
	existingGroupRules := make(map[string]map[string]interface{})

	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]interface{})
		if !ok {
			userRules = append(userRules, rule)
			continue
		}

		// Проверяем служебные правила.
		isService := false

		if action, ok := ruleMap["action"].(string); ok {
			if action == "sniff" || action == "hijack-dns" || action == "resolve" {
				serviceRules = append(serviceRules, rule)
				isService = true
			}
		}

		if _, ok := ruleMap["ip_is_private"]; ok {
			serviceRules = append(serviceRules, rule)
			isService = true
		}

		if isService {
			continue
		}

		// Проверяем, является ли это правилом группы.
		if ruleSetValue, ok := ruleMap["rule_set"].(string); ok {
			if strings.HasPrefix(ruleSetValue, "configurer-") {
				// Сохраняем правило группы.
				groupName := strings.TrimPrefix(ruleSetValue, "configurer-")
				existingGroupRules[groupName] = ruleMap
				continue
			}
		}

		// Сохраняем пользовательские правила.
		userRules = append(userRules, rule)
	}

	// Обновляем правило текущей группы.
	existingGroupRules[group.Name] = map[string]interface{}{
		"outbound": "select-" + group.Name,
		"rule_set": "configurer-" + group.Name,
	}

	// Добавляем правила групп в порядке из allGroups.
	var orderedGroupRules []interface{}

	for _, g := range allGroups {
		if ruleMap, exists := existingGroupRules[g.Name]; exists {
			orderedGroupRules = append(orderedGroupRules, ruleMap)
		}
	}

	// Собираем финальный список правил: служебные + пользовательские + группы.
	finalRules := append(serviceRules, userRules...)
	finalRules = append(finalRules, orderedGroupRules...)

	route["rules"] = finalRules

	// Обновляем или добавляем selector для данной группы.
	outbounds, ok := config["outbounds"].([]interface{})
	if !ok {
		return fmt.Errorf("outbounds section not found in config")
	}

	// Получаем список всех доступных outbounds и endpoints.
	var availableOutbounds []string
	existingOutboundTags := make(map[string]bool)

	for _, ob := range outbounds {
		obMap, ok := ob.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := obMap["tag"].(string)
		if !ok {
			continue
		}

		obType, ok := obMap["type"].(string)
		if !ok {
			continue
		}

		existingOutboundTags[tag] = true

		// Добавляем все outbounds кроме selector и dns-out.
		if obType != "selector" && tag != "dns-out" {
			availableOutbounds = append(availableOutbounds, tag)

			// Если это outbound с endpoints, добавляем их тоже.
			if endpoints, ok := obMap["endpoints"].([]interface{}); ok {
				for _, ep := range endpoints {
					epMap, ok := ep.(map[string]interface{})
					if !ok {
						continue
					}

					epTag, ok := epMap["tag"].(string)
					if !ok {
						continue
					}

					availableOutbounds = append(availableOutbounds, epTag)
					existingOutboundTags[epTag] = true
				}
			}
		}
	}

	// Определяем default outbound для группы.
	defaultOutbound := group.DefaultOutbound
	if defaultOutbound == "" {
		defaultOutbound = "direct"
	}

	// Проверяем, существует ли указанный default outbound.
	if !existingOutboundTags[defaultOutbound] {
		// Если удален, используем block.
		defaultOutbound = "block"
	}

	selectorTag := "select-" + group.Name
	foundSelector := false

	for i, ob := range outbounds {
		obMap, ok := ob.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := obMap["tag"].(string)
		if !ok {
			continue
		}

		if tag == selectorTag {
			// Обновляем существующий selector.
			obMap["default"] = defaultOutbound
			obMap["outbounds"] = availableOutbounds
			outbounds[i] = obMap
			foundSelector = true

			break
		}
	}

	if !foundSelector {
		// Добавляем новый selector.
		selector := map[string]interface{}{
			"default":                     defaultOutbound,
			"interrupt_exist_connections": true,
			"outbounds":                   availableOutbounds,
			"tag":                         selectorTag,
			"type":                        "selector",
		}

		outbounds = append(outbounds, selector)
	}

	config["outbounds"] = outbounds

	// Сохраняем конфиг обратно.
	configData, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	return nil
}
