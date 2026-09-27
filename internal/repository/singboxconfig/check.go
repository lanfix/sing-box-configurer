package singboxconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// GroupSyncStatus представляет статус синхронизации группы.
type GroupSyncStatus struct {
	Name            string `json:"name"`
	Synced          bool   `json:"synced"`
	HasRuleSet      bool   `json:"has_rule_set"`
	HasRule         bool   `json:"has_rule"`
	HasSelector     bool   `json:"has_selector"`
	DefaultOutbound string `json:"default_outbound"`
	ActualOutbound  string `json:"actual_outbound"`
	DNSSynced       bool   `json:"dns_synced"`
	DNSServer       string `json:"dns_server"`
	ActualDNSServer string `json:"actual_dns_server"`
}

// CheckGroupsSync проверяет состояние синхронизации групп в конфиге.
func (p *Provider) CheckGroupsSync(configPath string, groups []Group) ([]GroupSyncStatus, error) {
	// Читаем конфиг sing-box.
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	// Удаляем комментарии перед парсингом.
	cleanedConfigData := removeComments(configData)

	var config map[string]any

	if err := json.Unmarshal(cleanedConfigData, &config); err != nil {
		return nil, fmt.Errorf("cannot parse config: %w", err)
	}

	// Получаем секцию route.
	route, ok := config["route"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("route section not found in config")
	}

	// Получаем rule_set.
	var ruleSets []any

	if rs, ok := route["rule_set"].([]any); ok {
		ruleSets = rs
	}

	// Получаем rules.
	var rules []any

	if r, ok := route["rules"].([]any); ok {
		rules = r
	}

	// Получаем outbounds.
	outbounds, ok := config["outbounds"].([]any)
	if !ok {
		return nil, fmt.Errorf("outbounds section not found in config")
	}

	// Собираем информацию о существующих ruleset'ах.
	existingRuleSets := make(map[string]bool)

	for _, rs := range ruleSets {
		rsMap, ok := rs.(map[string]any)
		if !ok {
			continue
		}

		tag, ok := rsMap["tag"].(string)
		if !ok {
			continue
		}

		existingRuleSets[tag] = true
	}

	// Собираем информацию о существующих правилах: ключ — список тегов rule-set-ов правила.
	existingRules := make(map[string]bool)

	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]any)
		if !ok {
			continue
		}

		tags := extractRuleSetTags(ruleMap)
		if len(tags) == 0 {
			continue
		}

		existingRules[strings.Join(tags, ",")] = true
	}

	actualDNSServers := GetGroupDNSServers(config)
	hasHTTPSFilter := hasHTTPSFilterRule(config)

	// Собираем информацию о существующих селекторах и доступных outbounds.
	existingSelectors := make(map[string]string)
	existingOutboundTags := make(map[string]bool)

	for _, ob := range outbounds {
		obMap, ok := ob.(map[string]any)
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

		// Регистрируем все outbounds кроме dns-out.
		if tag != "dns-out" {
			existingOutboundTags[tag] = true
		}

		if obType == "selector" {
			defaultOutbound := ""

			if def, ok := obMap["default"].(string); ok {
				defaultOutbound = def
			}

			existingSelectors[tag] = defaultOutbound
		}
	}

	// Регистрируем endpoints, если есть.
	if endpoints, ok := config["endpoints"].([]any); ok {
		for _, ep := range endpoints {
			epMap, ok := ep.(map[string]any)
			if !ok {
				continue
			}

			epTag, ok := epMap["tag"].(string)
			if !ok {
				continue
			}

			existingOutboundTags[epTag] = true
		}
	}

	// Проверяем каждую группу.
	statuses := make([]GroupSyncStatus, 0, len(groups))

	for _, group := range groups {
		ruleSetTag := getRuleSetTagForGroup(group.Name)
		ipRuleSetTag := getIPRuleSetTagForGroup(group.Name)
		selectorTag := getSelectorTagForGroup(group.Name)

		hasRuleSet := existingRuleSets[ruleSetTag] && existingRuleSets[ipRuleSetTag]
		hasRule := existingRules[ruleSetTag+","+ipRuleSetTag]
		actualOutbound, hasSelector := existingSelectors[selectorTag]

		// Без DNS-сервера у группы не должно быть системных DNS-правил.
		actualDNSServer := actualDNSServers[group.Name]
		dnsSynced := actualDNSServer == group.DNSServer

		if group.DNSServer != "" {
			dnsSynced = dnsSynced && hasHTTPSFilter && hasGroupDNSRules(config, group)
		}

		// Нормализуем actualOutbound так же, как и при создании selector.
		if actualOutbound == "" {
			actualOutbound = "direct"
		}

		defaultOutbound := group.DefaultOutbound
		if defaultOutbound == "" {
			defaultOutbound = "direct"
		}

		// Проверяем, существует ли default outbound в конфиге.
		expectedOutbound := defaultOutbound
		if !existingOutboundTags[defaultOutbound] {
			expectedOutbound = "block"
		}

		synced := hasRuleSet && hasRule && hasSelector && (actualOutbound == expectedOutbound) && dnsSynced

		statuses = append(statuses, GroupSyncStatus{
			Name:            group.Name,
			Synced:          synced,
			HasRuleSet:      hasRuleSet,
			HasRule:         hasRule,
			HasSelector:     hasSelector,
			DefaultOutbound: expectedOutbound,
			ActualOutbound:  actualOutbound,
			DNSSynced:       dnsSynced,
			DNSServer:       group.DNSServer,
			ActualDNSServer: actualDNSServer,
		})
	}

	return statuses, nil
}
