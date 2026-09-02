package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// Rule represents a routing rule for VPN
type Rule struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // "domain", "domain_suffix", "ip", "cidr"
	Value       string    `json:"value"`
	Description string    `json:"description"`
	Applied     bool      `json:"applied"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`
}

// URLSource represents a URL source for rule lists
type URLSource struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Interval    int       `json:"interval"` // in minutes
	LastUpdate  time.Time `json:"last_update"`
	LastStatus  string    `json:"last_status"` // "success", "error"
	LastError   string    `json:"last_error,omitempty"`
	ItemsCount  uint64    `json:"items_count"`
	Applied     bool      `json:"applied"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`
}

// RulesData stores the rules and URL sources
type RulesData struct {
	Rules      []Rule      `json:"rules"`
	URLSources []URLSource `json:"url_sources,omitempty"`
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
	rulesPath        string
	sourceListsProxy func(r *http.Request) (*url.URL, error)
	urlRules         map[string]RuleSet // URL ID -> rules
	urlRulesMu       sync.RWMutex
	cancelFuncs      map[string]context.CancelFunc // URL ID -> cancel function
	cancelFuncsMu    sync.Mutex
}

func NewManager(rulesPath string, sourceListsProxyUrl string) (*Manager, error) {
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
		rulesPath:        rulesPath,
		sourceListsProxy: sourceListsProxy,
		urlRules:         map[string]RuleSet{},
		cancelFuncs:      map[string]context.CancelFunc{},
		data: RulesData{
			Rules:      []Rule{},
			URLSources: []URLSource{},
		},
	}, nil
}

func (rm *Manager) Load() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	data, err := os.ReadFile(rm.rulesPath)
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

	// Migration: if URLSources is nil, initialize it
	if newData.URLSources == nil {
		newData.URLSources = []URLSource{}
		log.Println("Migrated rules data: added URLSources field")
	}

	rm.data = newData

	return nil
}

func (rm *Manager) save() error {
	data, err := json.MarshalIndent(rm.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(rm.rulesPath, data, 0644)
}

func (rm *Manager) GetRules() []Rule {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.data.Rules
}

func (rm *Manager) AddRule(rule Rule) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rule.Applied = false
	rule.CreatedAt = time.Now()
	rm.data.Rules = append(rm.data.Rules, rule)

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

	// Build rule set from applied rules only
	var rules []map[string]interface{}

	// Collect all applied rules by type
	var domains []string
	var domainSuffixes []string
	var ipCidrs []string

	// Add manual rules (only applied rules, including those marked for deletion)
	for _, rule := range rm.data.Rules {
		// Skip only unapplied rules
		// Rules marked as Deleted but Applied should still be included until changes are applied
		if !rule.Applied {
			continue
		}

		// Add applied rule to corresponding list
		switch rule.Type {
		case "domain":
			domains = append(domains, rule.Value)
		case "domain_suffix":
			domainSuffixes = append(domainSuffixes, rule.Value)
		case "ip", "cidr":
			ipCidrs = append(ipCidrs, rule.Value)
		}
	}

	// Add URL source rules (from memory, only for applied sources)
	// Sources marked as Deleted but Applied should still be included until changes are applied
	rm.urlRulesMu.RLock()

	for sourceID, ruleSet := range rm.urlRules {
		for _, source := range rm.data.URLSources {
			if source.ID == sourceID && source.Applied {
				ipCidrs = append(ipCidrs, ruleSet.CidrList...)
				domains = append(domains, ruleSet.Domains...)
				domainSuffixes = append(domainSuffixes, ruleSet.DomainSuffixes...)

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
