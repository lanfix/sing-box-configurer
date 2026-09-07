package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Rule represents a routing rule for VPN
type Rule struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // "domain", "domain_suffix", "ip", "cidr"
	Value       string    `json:"value"`
	Description string    `json:"description"`
	Group       string    `json:"group"` // группа, к которой привязано правило
	Applied     bool      `json:"applied"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`
}

// URLSource represents a URL source for rule lists
type URLSource struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Group       string    `json:"group"`    // группа, к которой привязан источник
	Interval    int       `json:"interval"` // in minutes
	LastUpdate  time.Time `json:"last_update"`
	LastStatus  string    `json:"last_status"` // "success", "error"
	LastError   string    `json:"last_error,omitempty"`
	ItemsCount  uint64    `json:"items_count"`
	Applied     bool      `json:"applied"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`
}

// Group представляет логическую группу для правил.
type Group struct {
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DefaultOutbound string    `json:"default_outbound,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// RulesData stores the rules and URL sources
type RulesData struct {
	Rules      []Rule      `json:"rules"`
	URLSources []URLSource `json:"url_sources,omitempty"`
	Groups     []Group     `json:"groups,omitempty"`
}

// RuleVersion пятая версия формата правил.
// https://sing-box.sagernet.org/configuration/rule-set/source-format/#version
const RuleVersion = 4

type SingBoxRuleSet struct {
	// Version определяет формат правил, которые будет считывать sing-box.
	Version int                      `json:"version"`
	Rules   []map[string]interface{} `json:"rules"`
}

// Manager handles rules operations
type Manager struct {
	mu               sync.RWMutex
	data             RulesData
	appDataPath      string
	sourceListsProxy func(r *http.Request) (*url.URL, error)
	urlRules         map[string]RuleSet // URL ID -> rules
	urlRulesMu       sync.RWMutex
	cancelFuncs      map[string]context.CancelFunc // URL ID -> cancel function
	cancelFuncsMu    sync.Mutex
	migrated         bool // флаг, что была выполнена миграция
}

func NewManager(appDataPath, sourceListsProxyUrl string) (*Manager, error) {
	var sourceListsProxy func(r *http.Request) (*url.URL, error)

	if sourceListsProxyUrl != "" {
		proxyURL, err := url.Parse(sourceListsProxyUrl)
		if err != nil {
			return nil, fmt.Errorf("cannot parse source lists proxy url: %w", err)
		}

		sourceListsProxy = http.ProxyURL(proxyURL)

		log.Printf("using http proxy for source lists: %s", sourceListsProxyUrl)
	}

	return &Manager{
		appDataPath:      appDataPath,
		sourceListsProxy: sourceListsProxy,
		urlRules:         map[string]RuleSet{},
		cancelFuncs:      map[string]context.CancelFunc{},
		migrated:         false,
		data: RulesData{
			Rules:      []Rule{},
			URLSources: []URLSource{},
			Groups: []Group{
				{
					Name:        "default",
					Description: "Группа по умолчанию",
					CreatedAt:   time.Now(),
				},
			},
		},
	}, nil
}

func (rm *Manager) Load() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	data, err := os.ReadFile(rm.appDataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return rm.save()
		}

		return err
	}

	// Try to unmarshal into new format
	var newData RulesData
	if err := json.Unmarshal(data, &newData); err != nil {
		return err
	}

	migrationOccurred := false

	if newData.URLSources == nil {
		newData.URLSources = []URLSource{}
		migrationOccurred = true

		log.Println("Migrated rules data: added URLSources field")
	}

	if newData.Groups == nil {
		newData.Groups = []Group{
			{
				Name:        "default",
				Description: "Группа по умолчанию",
				CreatedAt:   time.Now(),
			},
		}

		migrationOccurred = true

		log.Println("Migrated rules data: added Groups field with default group")
	}

	for i := range newData.Rules {
		if newData.Rules[i].Group == "" {
			newData.Rules[i].Group = "default"
			migrationOccurred = true
		}
	}

	for i := range newData.URLSources {
		if newData.URLSources[i].Group == "" {
			newData.URLSources[i].Group = "default"
			migrationOccurred = true
		}
	}

	rm.data = newData

	if migrationOccurred {
		rm.migrated = true

		// Сохраняем изменения после миграции.
		if err := rm.save(); err != nil {
			log.Printf("Warning: failed to save migrated data: %v", err)
		}
	}

	return nil
}

// WasMigrated возвращает true, если при загрузке была выполнена миграция.
func (rm *Manager) WasMigrated() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.migrated
}

func (rm *Manager) save() error {
	data, err := json.MarshalIndent(rm.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(rm.appDataPath, data, 0644)
}

func (rm *Manager) GetRules() []Rule {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.data.Rules
}

func (rm *Manager) AddRule(rule Rule) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Проверяем, что группа существует.
	if !rm.groupExists(rule.Group) {
		return fmt.Errorf("группа %s не существует", rule.Group)
	}

	// Проверяем конфликты с существующими правилами.
	if err := rm.validateRuleConflicts(rule); err != nil {
		return err
	}

	rule.Applied = false
	rule.CreatedAt = time.Now()
	rm.data.Rules = append(rm.data.Rules, rule)

	return rm.save()
}

// BulkAddResult содержит результаты массового добавления правил.
type BulkAddResult struct {
	Success      int              `json:"success"`
	Failed       int              `json:"failed"`
	Total        int              `json:"total"`
	FailedValues []BulkAddFailure `json:"failed_values,omitempty"`
	AddedRules   []Rule           `json:"added_rules,omitempty"`
}

// BulkAddFailure содержит информацию об ошибке при добавлении правила.
type BulkAddFailure struct {
	Value string `json:"value"`
	Error string `json:"error"`
}

// AddRuleBulk добавляет несколько правил одновременно.
func (rm *Manager) AddRuleBulk(
	ruleType, values, description, group string,
) (*BulkAddResult, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.groupExists(group) {
		return nil, fmt.Errorf("группа %s не существует", group)
	}

	lines := strings.Split(values, "\n")
	result := &BulkAddResult{
		Total:        0,
		Success:      0,
		Failed:       0,
		FailedValues: []BulkAddFailure{},
		AddedRules:   []Rule{},
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		result.Total++

		rule := Rule{
			ID:          fmt.Sprintf("%s-%d", time.Now().Format("20060102150405"), result.Total),
			Type:        ruleType,
			Value:       line,
			Description: description,
			Group:       group,
			Applied:     false,
			CreatedAt:   time.Now(),
		}

		if err := rm.validateRuleConflicts(rule); err != nil {
			result.Failed++
			result.FailedValues = append(result.FailedValues, BulkAddFailure{
				Value: line,
				Error: err.Error(),
			})

			continue
		}

		rm.data.Rules = append(rm.data.Rules, rule)
		result.AddedRules = append(result.AddedRules, rule)
		result.Success++
	}

	if result.Success > 0 {
		if err := rm.save(); err != nil {
			return nil, fmt.Errorf("cannot save rules: %w", err)
		}
	}

	return result, nil
}

// EditRule обновляет параметры правила.
func (rm *Manager) EditRule(id string, description string, group string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Проверяем, что группа существует.
	if !rm.groupExists(group) {
		return fmt.Errorf("группа %s не существует", group)
	}

	// Находим правило.
	found := false

	for i := range rm.data.Rules {
		if rm.data.Rules[i].ID == id {
			rm.data.Rules[i].Description = description
			rm.data.Rules[i].Group = group
			found = true

			break
		}
	}

	if !found {
		return fmt.Errorf("правило с ID %s не найдено", id)
	}

	return rm.save()
}

func (rm *Manager) DeleteRule(id string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for i, rule := range rm.data.Rules {
		if rule.ID == id {
			rm.data.Rules[i].Deleted = true

			break
		}
	}

	return rm.save()
}

func (rm *Manager) ApplyRules() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Remove deleted rules and mark active ones as applied
	newRules := []Rule{}

	for i := range rm.data.Rules {
		rule := &rm.data.Rules[i]

		if rule.Deleted {
			continue
		}

		rule.Applied = true
		newRules = append(newRules, *rule)
	}

	// Update rules list (deleted rules are now permanently removed)
	rm.data.Rules = newRules

	// Save rules with updated applied status and deleted rules removed
	return rm.save()
}

func (rm *Manager) GetRuleSet() SingBoxRuleSet {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Deprecated: этот метод оставлен для обратной совместимости.
	// Используйте GetRuleSetByGroup для получения правил конкретной группы.
	return rm.getRuleSetByGroupLocked("default")
}

// GetRuleSetByGroup возвращает набор правил для указанной группы.
func (rm *Manager) GetRuleSetByGroup(groupName string) SingBoxRuleSet {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.getRuleSetByGroupLocked(groupName)
}

// getRuleSetByGroupLocked возвращает набор правил для указанной группы (без блокировки).
func (rm *Manager) getRuleSetByGroupLocked(groupName string) SingBoxRuleSet {
	// Build rule set from applied rules only
	var rules []map[string]interface{}

	// Collect all applied rules by type
	var domains []string
	var domainSuffixes []string
	var ipCidrs []string

	// Создаем карту для отслеживания конфликтов между ручными и автоматическими правилами.
	manualDomains := make(map[string]bool)
	manualSuffixes := make(map[string]bool)

	// Add manual rules (only applied rules, including those marked for deletion)
	for _, rule := range rm.data.Rules {
		// Skip only unapplied rules and rules from other groups
		// Rules marked as Deleted but Applied should still be included until changes are applied
		if !rule.Applied || rule.Group != groupName {
			continue
		}

		// Add applied rule to corresponding list
		switch rule.Type {
		case "domain":
			domains = append(domains, rule.Value)
			manualDomains[rule.Value] = true
		case "domain_suffix":
			domainSuffixes = append(domainSuffixes, rule.Value)
			manualSuffixes[rule.Value] = true
		case "ip", "cidr":
			ipCidrs = append(ipCidrs, rule.Value)
		}
	}

	// Add URL source rules (from memory, only for applied sources)
	// Sources marked as Deleted but Applied should still be included until changes are applied
	rm.urlRulesMu.RLock()

	for sourceID, ruleSet := range rm.urlRules {
		for _, source := range rm.data.URLSources {
			if source.ID == sourceID && source.Applied && source.Group == groupName {
				// Добавляем IP/CIDR без проверки конфликтов.
				ipCidrs = append(ipCidrs, ruleSet.CidrList...)

				// Проверяем конфликты для доменов.
				for _, domain := range ruleSet.Domains {
					if !manualDomains[domain] && !hasConflictWithManualRules(domain, "domain", manualDomains, manualSuffixes) {
						domains = append(domains, domain)
					}
				}

				// Проверяем конфликты для суффиксов.
				for _, suffix := range ruleSet.DomainSuffixes {
					if !manualSuffixes[suffix] && !hasConflictWithManualRules(suffix, "domain_suffix", manualDomains, manualSuffixes) {
						domainSuffixes = append(domainSuffixes, suffix)
					}
				}

				break
			}
		}
	}

	rm.urlRulesMu.RUnlock()

	// Create a single rule with all values
	if len(domains) > 0 || len(domainSuffixes) > 0 || len(ipCidrs) > 0 {
		rule := map[string]any{}

		if len(domains) > 0 {
			rule["domain"] = domains
		}

		if len(domainSuffixes) > 0 {
			rule["domain_suffix"] = domainSuffixes
		}

		if len(ipCidrs) > 0 {
			rule["ip_cidr"] = ipCidrs
		}

		rules = append(rules, rule)
	}

	return SingBoxRuleSet{
		Version: RuleVersion,
		Rules:   rules,
	}
}

// hasConflictWithManualRules проверяет, конфликтует ли правило с ручными правилами.
func hasConflictWithManualRules(value, ruleType string, manualDomains, manualSuffixes map[string]bool) bool {
	if ruleType == "domain" {
		// Проверяем, есть ли суффикс, который покрывает этот домен.
		for suffix := range manualSuffixes {
			if isDomainMatchesSuffix(value, suffix) {
				return true
			}
		}
	}

	if ruleType == "domain_suffix" {
		// Проверяем, есть ли домен, который конфликтует с этим суффиксом.
		for domain := range manualDomains {
			if isDomainMatchesSuffix(domain, value) {
				return true
			}
		}

		// Проверяем, есть ли суффикс, который конфликтует с этим суффиксом.
		for suffix := range manualSuffixes {
			if isDomainMatchesSuffix(value, suffix) || isDomainMatchesSuffix(suffix, value) {
				return true
			}
		}
	}

	return false
}

func (rm *Manager) GetPendingCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	count := 0
	for _, rule := range rm.data.Rules {
		if !rule.Applied || rule.Deleted {
			count++
		}
	}
	return count
}

// GetGroups возвращает список всех групп.
func (rm *Manager) GetGroups() []Group {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.data.Groups
}

// AddGroup добавляет новую группу.
func (rm *Manager) AddGroup(group Group) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Проверяем, что группа с таким именем не существует.
	for _, g := range rm.data.Groups {
		if g.Name == group.Name {
			return fmt.Errorf("группа с именем %s уже существует", group.Name)
		}
	}

	group.CreatedAt = time.Now()
	rm.data.Groups = append(rm.data.Groups, group)

	return rm.save()
}

// EditGroup обновляет параметры группы.
func (rm *Manager) EditGroup(name string, description string, defaultOutbound string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Находим группу.
	found := false

	for i := range rm.data.Groups {
		if rm.data.Groups[i].Name == name {
			rm.data.Groups[i].Description = description
			rm.data.Groups[i].DefaultOutbound = defaultOutbound
			found = true

			break
		}
	}

	if !found {
		return fmt.Errorf("группа %s не найдена", name)
	}

	return rm.save()
}

// DeleteGroup удаляет группу.
func (rm *Manager) DeleteGroup(name string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if name == "default" {
		return fmt.Errorf("нельзя удалить группу default")
	}

	// Проверяем, что в группе нет правил и источников.
	for _, rule := range rm.data.Rules {
		if rule.Group == name && !rule.Deleted {
			return fmt.Errorf("в группе %s есть правила, удалите их сначала", name)
		}
	}

	for _, source := range rm.data.URLSources {
		if source.Group == name && !source.Deleted {
			return fmt.Errorf("в группе %s есть источники, удалите их сначала", name)
		}
	}

	// Удаляем группу.
	newGroups := []Group{}

	for _, g := range rm.data.Groups {
		if g.Name != name {
			newGroups = append(newGroups, g)
		}
	}

	rm.data.Groups = newGroups

	return rm.save()
}

// groupExists проверяет, существует ли группа с заданным именем.
func (rm *Manager) groupExists(name string) bool {
	for _, g := range rm.data.Groups {
		if g.Name == name {
			return true
		}
	}

	return false
}

// validateRuleConflicts проверяет конфликты правил domain и domain_suffix глобально.
func (rm *Manager) validateRuleConflicts(newRule Rule) error {
	// Проверяем только для domain и domain_suffix.
	if newRule.Type != "domain" && newRule.Type != "domain_suffix" {
		return nil
	}

	for _, rule := range rm.data.Rules {
		// Пропускаем удаленные правила.
		if rule.Deleted {
			continue
		}

		// Проверяем дубликаты глобально.
		if rule.Type == newRule.Type && rule.Value == newRule.Value {
			if rule.Group == newRule.Group {
				return fmt.Errorf("правило %s %s уже существует в группе %s", newRule.Type, newRule.Value, newRule.Group)
			}

			return fmt.Errorf("правило %s %s уже существует в другой группе %s", newRule.Type, newRule.Value, rule.Group)
		}

		// Проверяем конфликты domain и domain_suffix глобально.
		if newRule.Type == "domain" && rule.Type == "domain_suffix" {
			if isDomainMatchesSuffix(newRule.Value, rule.Value) {
				return fmt.Errorf("домен %s конфликтует с существующим суффиксом %s в группе %s", newRule.Value, rule.Value, rule.Group)
			}
		}

		if newRule.Type == "domain_suffix" && rule.Type == "domain" {
			if isDomainMatchesSuffix(rule.Value, newRule.Value) {
				return fmt.Errorf("суффикс %s конфликтует с существующим доменом %s в группе %s", newRule.Value, rule.Value, rule.Group)
			}
		}

		if newRule.Type == "domain_suffix" && rule.Type == "domain_suffix" {
			if isDomainMatchesSuffix(newRule.Value, rule.Value) || isDomainMatchesSuffix(rule.Value, newRule.Value) {
				return fmt.Errorf("суффикс %s конфликтует с существующим суффиксом %s в группе %s", newRule.Value, rule.Value, rule.Group)
			}
		}
	}

	return nil
}

// isDomainMatchesSuffix проверяет, является ли домен поддоменом суффикса.
func isDomainMatchesSuffix(domain, suffix string) bool {
	// example.com совпадает с суффиксом example.com.
	if domain == suffix {
		return true
	}

	// test.example.com совпадает с суффиксом example.com.
	if strings.HasSuffix(domain, "."+suffix) {
		return true
	}

	return false
}
