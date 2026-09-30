package rules

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
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

	// Detour — outbound sing-box, через который загружается список (например, VPN для заблокированного
	// сайта). Пустой — загрузка напрямую из конфигуратора.
	Detour string `json:"detour,omitempty"`
}

// Group представляет логическую группу для правил.
type Group struct {
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DefaultOutbound string    `json:"default_outbound,omitempty"`
	DNSServer       string    `json:"dns_server,omitempty"` // тег DNS-сервера для доменов группы
	CreatedAt       time.Time `json:"created_at"`

	// System — системная группа: не хранится в app.json, не редактируется и не удаляется.
	System bool `json:"system,omitempty"`
}

// RulesData stores the rules and URL sources
type RulesData struct {
	Rules      []Rule      `json:"rules"`
	URLSources []URLSource `json:"url_sources"`
	Groups     []Group     `json:"groups"`
}

const (
	// BlockGroupName — системная группа: соединения по ее правилам отклоняются.
	BlockGroupName = "block"

	// BypassGroupName — системная группа: трафик по ее правилам идет мимо туннеля sing-box.
	BypassGroupName = "bypass"

	// DefaultGroupName — группа, которая создается при первой инициализации. В нее же попадали правила
	// до появления групп.
	DefaultGroupName = "default"

	// Outbound и DNS-сервер группы default новой инсталляции.
	defaultGroupOutbound  = "auto"
	defaultGroupDNSServer = "cloudflare"
)

// groupNameRe — допустимое имя группы: оно входит в теги rule-set-ов и selector-а.
var groupNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// SystemGroups возвращает системные группы.
func SystemGroups() []Group {
	return []Group{
		{
			Name:            BlockGroupName,
			Description:     "Блокировка: соединения отклоняются",
			DefaultOutbound: "",
			DNSServer:       "",
			CreatedAt:       time.Time{},
			System:          true,
		},
		{
			Name:            BypassGroupName,
			Description:     "Мимо туннеля: sing-box не перехватывает трафик",
			DefaultOutbound: "",
			DNSServer:       "",
			CreatedAt:       time.Time{},
			System:          true,
		},
	}
}

// IsSystemGroup проверяет, что имя принадлежит системной группе.
func IsSystemGroup(name string) bool {
	return name == BlockGroupName || name == BypassGroupName
}

// RuleVersion пятая версия формата правил.
// https://sing-box.sagernet.org/configuration/rule-set/source-format/#version
const RuleVersion = 4

// RuleSetKind определяет, какие типы значений попадают в набор правил.
type RuleSetKind string

const (
	// RuleSetKindAll — домены, суффиксы и IP/CIDR в одном наборе.
	RuleSetKindAll RuleSetKind = ""

	// RuleSetKindDomain — только домены и суффиксы. Такой набор можно использовать в DNS-правилах
	// без legacy address filter.
	RuleSetKindDomain RuleSetKind = "domain"

	// RuleSetKindIP — только IP/CIDR.
	RuleSetKindIP RuleSetKind = "ip"
)

// ErrRuleSetNotReady возвращается, пока идет первая загрузка URL-источника набора, у которого нет кэша на диске.
// Неполный набор отдавать нельзя: sing-box закэширует его и отправит трафик мимо туннеля.
var ErrRuleSetNotReady = errors.New("rule-set is not ready")

// includesDomains проверяет, попадают ли в набор домены и суффиксы.
func (k RuleSetKind) includesDomains() bool {
	return k != RuleSetKindIP
}

// includesIPs проверяет, попадают ли в набор IP/CIDR.
func (k RuleSetKind) includesIPs() bool {
	return k != RuleSetKindDomain
}

type SingBoxRuleSet struct {
	// Version определяет формат правил, которые будет считывать sing-box.
	Version int                      `json:"version"`
	Rules   []map[string]interface{} `json:"rules"`
}

// DetourProxy возвращает адрес прокси, через который загружается источник с detour (outbound sing-box).
type DetourProxy func(detour string) (*url.URL, error)

// Options — параметры менеджера правил.
type Options struct {
	// SourceListsProxyURL — прокси для загрузки источников без detour (необязательно).
	SourceListsProxyURL string

	// CacheDir — каталог кэша загруженных списков источников. Пустой — кэш выключен.
	CacheDir string

	// DetourProxy — прокси для источников с detour. nil — detour не поддерживается.
	DetourProxy DetourProxy
}

// Manager handles rules operations
type Manager struct {
	mu               sync.RWMutex
	data             RulesData
	appData          *appdata.File
	sourceListsProxy func(r *http.Request) (*url.URL, error)
	detourProxy      DetourProxy
	cacheDir         string
	urlRules         map[string]RuleSet // URL ID -> rules
	urlRulesMu       sync.RWMutex
	cancelFuncs      map[string]context.CancelFunc // URL ID -> cancel function
	cancelFuncsMu    sync.Mutex

	// attempted — источники, загрузка которых уже выполнялась после старта (под urlRulesMu).
	attempted map[string]bool
}

// NewManager создает менеджер правил.
func NewManager(appData *appdata.File, opts Options) (*Manager, error) {
	var sourceListsProxy func(r *http.Request) (*url.URL, error)

	if opts.SourceListsProxyURL != "" {
		proxyURL, err := url.Parse(opts.SourceListsProxyURL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse source lists proxy url: %w", err)
		}

		sourceListsProxy = http.ProxyURL(proxyURL)

		log.Printf("using http proxy for source lists: %s", opts.SourceListsProxyURL)
	}

	return &Manager{
		appData:          appData,
		sourceListsProxy: sourceListsProxy,
		detourProxy:      opts.DetourProxy,
		cacheDir:         opts.CacheDir,
		urlRules:         map[string]RuleSet{},
		cancelFuncs:      map[string]context.CancelFunc{},
		attempted:        map[string]bool{},
		data: RulesData{
			Rules:      []Rule{},
			URLSources: []URLSource{},
			Groups:     initialGroups(),
		},
	}, nil
}

// initialGroups возвращает группы новой инсталляции (кроме системных block и bypass).
func initialGroups() []Group {
	return []Group{
		{
			Name:            DefaultGroupName,
			Description:     "Группа по умолчанию",
			DefaultOutbound: defaultGroupOutbound,
			DNSServer:       defaultGroupDNSServer,
			CreatedAt:       time.Now(),
			System:          false,
		},
	}
}

// legacyGroups возвращает группы для данных, созданных до появления групп: все правила попадают в default,
// и их трафик, как и раньше, идет напрямую.
func legacyGroups() []Group {
	return []Group{
		{
			Name:            DefaultGroupName,
			Description:     "Группа по умолчанию",
			DefaultOutbound: "",
			DNSServer:       "",
			CreatedAt:       time.Now(),
			System:          false,
		},
	}
}

func (rm *Manager) Load() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	var newData RulesData

	if err := rm.appData.Read(&newData); err != nil {
		if errors.Is(err, appdata.ErrNotExist) {
			return rm.save()
		}

		return err
	}

	changed := false

	if newData.Rules == nil {
		newData.Rules = []Rule{}
		changed = true
	}

	if newData.URLSources == nil {
		newData.URLSources = []URLSource{}
		changed = true
	}

	// Данных о группах нет: это новая инсталляция либо данные, созданные до появления групп.
	if newData.Groups == nil {
		newData.Groups = initialGroups()
		changed = true

		if len(newData.Rules) > 0 || len(newData.URLSources) > 0 {
			newData.Groups = legacyGroups()
		}
	}

	for i := range newData.Rules {
		if newData.Rules[i].Group == "" {
			newData.Rules[i].Group = DefaultGroupName
			changed = true
		}
	}

	for i := range newData.URLSources {
		if newData.URLSources[i].Group == "" {
			newData.URLSources[i].Group = DefaultGroupName
			changed = true
		}
	}

	rm.data = newData

	if changed {
		if err := rm.save(); err != nil {
			log.Printf("Warning: failed to save normalized rules data: %v", err)
		}
	}

	rm.loadCachedSources()

	return nil
}

// save сохраняет правила, URL-источники и группы в app.json, не затрагивая данные других менеджеров.
func (rm *Manager) save() error {
	return rm.appData.Merge(rm.data)
}

func (rm *Manager) GetRules() []Rule {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Возвращаем копию, чтобы вызывающий код не читал слайс, который меняют под блокировкой.
	return slices.Clone(rm.data.Rules)
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

// GetRuleSetByGroup возвращает набор правил вида kind для указанной группы.
func (rm *Manager) GetRuleSetByGroup(groupName string, kind RuleSetKind) (SingBoxRuleSet, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.getRuleSetLocked(kind, groupName)
}

// GetBypassRuleSet возвращает набор правил системной группы bypass (мимо туннеля).
func (rm *Manager) GetBypassRuleSet() (SingBoxRuleSet, error) {
	return rm.GetRuleSetByGroup(BypassGroupName, RuleSetKindAll)
}

// getRuleSetLocked собирает набор вида kind из примененных правил и источников группы groupName
// (без блокировки). Если какой-то из источников еще не загружен после старта, возвращает ErrRuleSetNotReady.
func (rm *Manager) getRuleSetLocked(kind RuleSetKind, groupName string) (SingBoxRuleSet, error) {
	rules := make([]map[string]any, 0, 1)

	var (
		domains        []string
		domainSuffixes []string
		ipCidrs        []string
	)

	// Карты ручных правил для отсечения конфликтующих значений из источников.
	manualDomains := make(map[string]bool)
	manualSuffixes := make(map[string]bool)

	// Правила, помеченные на удаление, но примененные, остаются в наборе до применения изменений.
	for _, rule := range rm.data.Rules {
		if !rule.Applied || rule.Group != groupName {
			continue
		}

		switch rule.Type {
		case "domain":
			if kind.includesDomains() {
				domains = append(domains, rule.Value)
				manualDomains[rule.Value] = true
			}

		case "domain_suffix":
			if kind.includesDomains() {
				domainSuffixes = append(domainSuffixes, rule.Value)
				manualSuffixes[rule.Value] = true
			}

		case "ip", "cidr":
			if kind.includesIPs() {
				ipCidrs = append(ipCidrs, rule.Value)
			}
		}
	}

	// Источники, еще не загруженные после старта.
	var notLoaded []string

	// Источники, помеченные на удаление, но примененные, также остаются в наборе.
	rm.urlRulesMu.RLock()

	for _, source := range rm.data.URLSources {
		if !source.Applied || source.Group != groupName {
			continue
		}

		ruleSet, ok := rm.urlRules[source.ID]

		// Источник без кэша, который не загрузился и после попытки, не держит всю группу: прежде
		// в наборе его тоже не было, а без набора sing-box может не запуститься.
		if !ok && !rm.attempted[source.ID] {
			notLoaded = append(notLoaded, source.Description)

			continue
		}

		if !ok {
			continue
		}

		// IP/CIDR добавляем без проверки конфликтов.
		if kind.includesIPs() {
			ipCidrs = append(ipCidrs, ruleSet.CidrList...)
		}

		if !kind.includesDomains() {
			continue
		}

		for _, domain := range ruleSet.Domains {
			if !manualDomains[domain] && !hasConflictWithManualRules(domain, "domain", manualDomains, manualSuffixes) {
				domains = append(domains, domain)
			}
		}

		for _, suffix := range ruleSet.DomainSuffixes {
			if !manualSuffixes[suffix] && !hasConflictWithManualRules(suffix, "domain_suffix", manualDomains, manualSuffixes) {
				domainSuffixes = append(domainSuffixes, suffix)
			}
		}
	}

	rm.urlRulesMu.RUnlock()

	if len(notLoaded) > 0 {
		return SingBoxRuleSet{}, fmt.Errorf("%w: url sources not loaded: %s", ErrRuleSetNotReady, strings.Join(notLoaded, ", "))
	}

	// Все значения собираются в одно правило.
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
	}, nil
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

// GetGroups возвращает пользовательские группы (без системных) в порядке их создания.
func (rm *Manager) GetGroups() []Group {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Возвращаем копию, чтобы вызывающий код не читал слайс, который меняют под блокировкой.
	return slices.Clone(rm.data.Groups)
}

// GetAllGroups возвращает системные группы, а за ними пользовательские.
func (rm *Manager) GetAllGroups() []Group {
	return append(SystemGroups(), rm.GetGroups()...)
}

// GroupsByDNSServer возвращает имена пользовательских групп, у которых DNS-сервер равен dnsServer.
func (rm *Manager) GroupsByDNSServer(dnsServer string) []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	names := make([]string, 0)

	for _, group := range rm.data.Groups {
		if group.DNSServer == dnsServer {
			names = append(names, group.Name)
		}
	}

	return names
}

// AddGroup добавляет новую группу.
func (rm *Manager) AddGroup(group Group) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	group.Name = strings.TrimSpace(group.Name)

	if !groupNameRe.MatchString(group.Name) {
		return fmt.Errorf("некорректное имя группы %q: допустимы латиница, цифры, дефис и подчеркивание", group.Name)
	}

	if IsSystemGroup(group.Name) {
		return fmt.Errorf("имя группы %s зарезервировано", group.Name)
	}

	// Проверяем, что группа с таким именем не существует.
	for _, g := range rm.data.Groups {
		if g.Name == group.Name {
			return fmt.Errorf("группа с именем %s уже существует", group.Name)
		}
	}

	group.System = false
	group.CreatedAt = time.Now()
	rm.data.Groups = append(rm.data.Groups, group)

	return rm.save()
}

// EditGroup обновляет параметры группы.
func (rm *Manager) EditGroup(name, description, defaultOutbound, dnsServer string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if IsSystemGroup(name) {
		return fmt.Errorf("системную группу %s нельзя изменить", name)
	}

	// Находим группу.
	found := false

	for i := range rm.data.Groups {
		if rm.data.Groups[i].Name == name {
			rm.data.Groups[i].Description = description
			rm.data.Groups[i].DefaultOutbound = defaultOutbound
			rm.data.Groups[i].DNSServer = dnsServer
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

	if IsSystemGroup(name) {
		return fmt.Errorf("системную группу %s нельзя удалить", name)
	}

	// Удалить можно только пустую группу.
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

// groupExists проверяет, существует ли группа (пользовательская или системная) с заданным именем.
func (rm *Manager) groupExists(name string) bool {
	if IsSystemGroup(name) {
		return true
	}

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
