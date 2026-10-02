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

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// Rule represents a routing rule for VPN
type Rule struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // "domain", "domain_suffix", "ip", "cidr"
	Value       string    `json:"value"`
	Description string    `json:"description"`
	Group       string    `json:"group"` // РіСЂСѓРїРїР°, Рє РєРѕС‚РѕСЂРѕР№ РїСЂРёРІСЏР·Р°РЅРѕ РїСЂР°РІРёР»Рѕ
	Applied     bool      `json:"applied"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`
}

// URLSource represents a URL source for rule lists
type URLSource struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Group       string    `json:"group"`    // РіСЂСѓРїРїР°, Рє РєРѕС‚РѕСЂРѕР№ РїСЂРёРІСЏР·Р°РЅ РёСЃС‚РѕС‡РЅРёРє
	Interval    int       `json:"interval"` // in minutes
	LastUpdate  time.Time `json:"last_update"`
	LastStatus  string    `json:"last_status"` // "success", "error"
	LastError   string    `json:"last_error,omitempty"`
	ItemsCount  uint64    `json:"items_count"`
	Applied     bool      `json:"applied"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`

	// Detour вЂ” outbound sing-box, С‡РµСЂРµР· РєРѕС‚РѕСЂС‹Р№ Р·Р°РіСЂСѓР¶Р°РµС‚СЃСЏ СЃРїРёСЃРѕРє (РЅР°РїСЂРёРјРµСЂ, VPN РґР»СЏ Р·Р°Р±Р»РѕРєРёСЂРѕРІР°РЅРЅРѕРіРѕ
	// СЃР°Р№С‚Р°). РџСѓСЃС‚РѕР№ вЂ” Р·Р°РіСЂСѓР·РєР° РЅР°РїСЂСЏРјСѓСЋ РёР· РєРѕРЅС„РёРіСѓСЂР°С‚РѕСЂР°.
	Detour string `json:"detour,omitempty"`
}

// Group РїСЂРµРґСЃС‚Р°РІР»СЏРµС‚ Р»РѕРіРёС‡РµСЃРєСѓСЋ РіСЂСѓРїРїСѓ РґР»СЏ РїСЂР°РІРёР».
type Group struct {
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DefaultOutbound string    `json:"default_outbound,omitempty"`
	DNSServer       string    `json:"dns_server,omitempty"` // С‚РµРі DNS-СЃРµСЂРІРµСЂР° РґР»СЏ РґРѕРјРµРЅРѕРІ РіСЂСѓРїРїС‹
	CreatedAt       time.Time `json:"created_at"`

	// System вЂ” СЃРёСЃС‚РµРјРЅР°СЏ РіСЂСѓРїРїР°: РЅРµ С…СЂР°РЅРёС‚СЃСЏ РІ app.json, РЅРµ СЂРµРґР°РєС‚РёСЂСѓРµС‚СЃСЏ Рё РЅРµ СѓРґР°Р»СЏРµС‚СЃСЏ.
	System bool `json:"system,omitempty"`
}

// RulesData stores the rules and URL sources
type RulesData struct {
	Rules      []Rule      `json:"rules"`
	URLSources []URLSource `json:"url_sources"`
	Groups     []Group     `json:"groups"`
}

const (
	// BlockGroupName вЂ” СЃРёСЃС‚РµРјРЅР°СЏ РіСЂСѓРїРїР°: СЃРѕРµРґРёРЅРµРЅРёСЏ РїРѕ РµРµ РїСЂР°РІРёР»Р°Рј РѕС‚РєР»РѕРЅСЏСЋС‚СЃСЏ.
	BlockGroupName = "block"

	// BypassGroupName вЂ” СЃРёСЃС‚РµРјРЅР°СЏ РіСЂСѓРїРїР°: С‚СЂР°С„РёРє РїРѕ РµРµ РїСЂР°РІРёР»Р°Рј РёРґРµС‚ РјРёРјРѕ С‚СѓРЅРЅРµР»СЏ sing-box.
	BypassGroupName = "bypass"

	// DefaultGroupName вЂ” РіСЂСѓРїРїР°, РєРѕС‚РѕСЂР°СЏ СЃРѕР·РґР°РµС‚СЃСЏ РїСЂРё РїРµСЂРІРѕР№ РёРЅРёС†РёР°Р»РёР·Р°С†РёРё. Р’ РЅРµРµ Р¶Рµ РїРѕРїР°РґР°Р»Рё РїСЂР°РІРёР»Р°
	// РґРѕ РїРѕСЏРІР»РµРЅРёСЏ РіСЂСѓРїРї.
	DefaultGroupName = "default"

	// Outbound Рё DNS-СЃРµСЂРІРµСЂ РіСЂСѓРїРїС‹ default РЅРѕРІРѕР№ РёРЅСЃС‚Р°Р»Р»СЏС†РёРё.
	defaultGroupOutbound  = "auto"
	defaultGroupDNSServer = "cloudflare"
)

// groupNameRe вЂ” РґРѕРїСѓСЃС‚РёРјРѕРµ РёРјСЏ РіСЂСѓРїРїС‹: РѕРЅРѕ РІС…РѕРґРёС‚ РІ С‚РµРіРё rule-set-РѕРІ Рё selector-Р°.
var groupNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// SystemGroups РІРѕР·РІСЂР°С‰Р°РµС‚ СЃРёСЃС‚РµРјРЅС‹Рµ РіСЂСѓРїРїС‹.
func SystemGroups() []Group {
	return []Group{
		{
			Name:            BlockGroupName,
			Description:     "Р‘Р»РѕРєРёСЂРѕРІРєР°: СЃРѕРµРґРёРЅРµРЅРёСЏ РѕС‚РєР»РѕРЅСЏСЋС‚СЃСЏ",
			DefaultOutbound: "",
			DNSServer:       "",
			CreatedAt:       time.Time{},
			System:          true,
		},
		{
			Name:            BypassGroupName,
			Description:     "РњРёРјРѕ С‚СѓРЅРЅРµР»СЏ: sing-box РЅРµ РїРµСЂРµС…РІР°С‚С‹РІР°РµС‚ С‚СЂР°С„РёРє",
			DefaultOutbound: "",
			DNSServer:       "",
			CreatedAt:       time.Time{},
			System:          true,
		},
	}
}

// IsSystemGroup РїСЂРѕРІРµСЂСЏРµС‚, С‡С‚Рѕ РёРјСЏ РїСЂРёРЅР°РґР»РµР¶РёС‚ СЃРёСЃС‚РµРјРЅРѕР№ РіСЂСѓРїРїРµ.
func IsSystemGroup(name string) bool {
	return name == BlockGroupName || name == BypassGroupName
}

// RuleVersion РїСЏС‚Р°СЏ РІРµСЂСЃРёСЏ С„РѕСЂРјР°С‚Р° РїСЂР°РІРёР».
// https://sing-box.sagernet.org/configuration/rule-set/source-format/#version
const RuleVersion = 4

// RuleSetKind РѕРїСЂРµРґРµР»СЏРµС‚, РєР°РєРёРµ С‚РёРїС‹ Р·РЅР°С‡РµРЅРёР№ РїРѕРїР°РґР°СЋС‚ РІ РЅР°Р±РѕСЂ РїСЂР°РІРёР».
type RuleSetKind string

const (
	// RuleSetKindAll вЂ” РґРѕРјРµРЅС‹, СЃСѓС„С„РёРєСЃС‹ Рё IP/CIDR РІ РѕРґРЅРѕРј РЅР°Р±РѕСЂРµ.
	RuleSetKindAll RuleSetKind = ""

	// RuleSetKindDomain вЂ” С‚РѕР»СЊРєРѕ РґРѕРјРµРЅС‹ Рё СЃСѓС„С„РёРєСЃС‹. РўР°РєРѕР№ РЅР°Р±РѕСЂ РјРѕР¶РЅРѕ РёСЃРїРѕР»СЊР·РѕРІР°С‚СЊ РІ DNS-РїСЂР°РІРёР»Р°С…
	// Р±РµР· legacy address filter.
	RuleSetKindDomain RuleSetKind = "domain"

	// RuleSetKindIP вЂ” С‚РѕР»СЊРєРѕ IP/CIDR.
	RuleSetKindIP RuleSetKind = "ip"
)

// ErrRuleSetNotReady РІРѕР·РІСЂР°С‰Р°РµС‚СЃСЏ, РїРѕРєР° РёРґРµС‚ РїРµСЂРІР°СЏ Р·Р°РіСЂСѓР·РєР° URL-РёСЃС‚РѕС‡РЅРёРєР° РЅР°Р±РѕСЂР°, Сѓ РєРѕС‚РѕСЂРѕРіРѕ РЅРµС‚ РєСЌС€Р° РЅР° РґРёСЃРєРµ.
// РќРµРїРѕР»РЅС‹Р№ РЅР°Р±РѕСЂ РѕС‚РґР°РІР°С‚СЊ РЅРµР»СЊР·СЏ: sing-box Р·Р°РєСЌС€РёСЂСѓРµС‚ РµРіРѕ Рё РѕС‚РїСЂР°РІРёС‚ С‚СЂР°С„РёРє РјРёРјРѕ С‚СѓРЅРЅРµР»СЏ.
var ErrRuleSetNotReady = errors.New("rule-set is not ready")

// includesDomains РїСЂРѕРІРµСЂСЏРµС‚, РїРѕРїР°РґР°СЋС‚ Р»Рё РІ РЅР°Р±РѕСЂ РґРѕРјРµРЅС‹ Рё СЃСѓС„С„РёРєСЃС‹.
func (k RuleSetKind) includesDomains() bool {
	return k != RuleSetKindIP
}

// includesIPs РїСЂРѕРІРµСЂСЏРµС‚, РїРѕРїР°РґР°СЋС‚ Р»Рё РІ РЅР°Р±РѕСЂ IP/CIDR.
func (k RuleSetKind) includesIPs() bool {
	return k != RuleSetKindDomain
}

type SingBoxRuleSet struct {
	// Version РѕРїСЂРµРґРµР»СЏРµС‚ С„РѕСЂРјР°С‚ РїСЂР°РІРёР», РєРѕС‚РѕСЂС‹Рµ Р±СѓРґРµС‚ СЃС‡РёС‚С‹РІР°С‚СЊ sing-box.
	Version int                      `json:"version"`
	Rules   []map[string]interface{} `json:"rules"`
}

// DetourProxy РІРѕР·РІСЂР°С‰Р°РµС‚ Р°РґСЂРµСЃ РїСЂРѕРєСЃРё, С‡РµСЂРµР· РєРѕС‚РѕСЂС‹Р№ Р·Р°РіСЂСѓР¶Р°РµС‚СЃСЏ РёСЃС‚РѕС‡РЅРёРє СЃ detour (outbound sing-box).
type DetourProxy func(detour string) (*url.URL, error)

// Options вЂ” РїР°СЂР°РјРµС‚СЂС‹ РјРµРЅРµРґР¶РµСЂР° РїСЂР°РІРёР».
type Options struct {
	// SourceListsProxyURL вЂ” РїСЂРѕРєСЃРё РґР»СЏ Р·Р°РіСЂСѓР·РєРё РёСЃС‚РѕС‡РЅРёРєРѕРІ Р±РµР· detour (РЅРµРѕР±СЏР·Р°С‚РµР»СЊРЅРѕ).
	SourceListsProxyURL string

	// CacheDir вЂ” РєР°С‚Р°Р»РѕРі РєСЌС€Р° Р·Р°РіСЂСѓР¶РµРЅРЅС‹С… СЃРїРёСЃРєРѕРІ РёСЃС‚РѕС‡РЅРёРєРѕРІ. РџСѓСЃС‚РѕР№ вЂ” РєСЌС€ РІС‹РєР»СЋС‡РµРЅ.
	CacheDir string

	// DetourProxy вЂ” РїСЂРѕРєСЃРё РґР»СЏ РёСЃС‚РѕС‡РЅРёРєРѕРІ СЃ detour. nil вЂ” detour РЅРµ РїРѕРґРґРµСЂР¶РёРІР°РµС‚СЃСЏ.
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

	// attempted вЂ” РёСЃС‚РѕС‡РЅРёРєРё, Р·Р°РіСЂСѓР·РєР° РєРѕС‚РѕСЂС‹С… СѓР¶Рµ РІС‹РїРѕР»РЅСЏР»Р°СЃСЊ РїРѕСЃР»Рµ СЃС‚Р°СЂС‚Р° (РїРѕРґ urlRulesMu).
	attempted map[string]bool
}

// NewManager СЃРѕР·РґР°РµС‚ РјРµРЅРµРґР¶РµСЂ РїСЂР°РІРёР».
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

// initialGroups РІРѕР·РІСЂР°С‰Р°РµС‚ РіСЂСѓРїРїС‹ РЅРѕРІРѕР№ РёРЅСЃС‚Р°Р»Р»СЏС†РёРё (РєСЂРѕРјРµ СЃРёСЃС‚РµРјРЅС‹С… block Рё bypass).
func initialGroups() []Group {
	return []Group{
		{
			Name:            DefaultGroupName,
			Description:     "Р“СЂСѓРїРїР° РїРѕ СѓРјРѕР»С‡Р°РЅРёСЋ",
			DefaultOutbound: defaultGroupOutbound,
			DNSServer:       defaultGroupDNSServer,
			CreatedAt:       time.Now(),
			System:          false,
		},
	}
}

// legacyGroups РІРѕР·РІСЂР°С‰Р°РµС‚ РіСЂСѓРїРїС‹ РґР»СЏ РґР°РЅРЅС‹С…, СЃРѕР·РґР°РЅРЅС‹С… РґРѕ РїРѕСЏРІР»РµРЅРёСЏ РіСЂСѓРїРї: РІСЃРµ РїСЂР°РІРёР»Р° РїРѕРїР°РґР°СЋС‚ РІ default,
// Рё РёС… С‚СЂР°С„РёРє, РєР°Рє Рё СЂР°РЅСЊС€Рµ, РёРґРµС‚ РЅР°РїСЂСЏРјСѓСЋ.
func legacyGroups() []Group {
	return []Group{
		{
			Name:            DefaultGroupName,
			Description:     "Р“СЂСѓРїРїР° РїРѕ СѓРјРѕР»С‡Р°РЅРёСЋ",
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

	// Р”Р°РЅРЅС‹С… Рѕ РіСЂСѓРїРїР°С… РЅРµС‚: СЌС‚Рѕ РЅРѕРІР°СЏ РёРЅСЃС‚Р°Р»Р»СЏС†РёСЏ Р»РёР±Рѕ РґР°РЅРЅС‹Рµ, СЃРѕР·РґР°РЅРЅС‹Рµ РґРѕ РїРѕСЏРІР»РµРЅРёСЏ РіСЂСѓРїРї.
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

// save СЃРѕС…СЂР°РЅСЏРµС‚ РїСЂР°РІРёР»Р°, URL-РёСЃС‚РѕС‡РЅРёРєРё Рё РіСЂСѓРїРїС‹ РІ app.json, РЅРµ Р·Р°С‚СЂР°РіРёРІР°СЏ РґР°РЅРЅС‹Рµ РґСЂСѓРіРёС… РјРµРЅРµРґР¶РµСЂРѕРІ.
func (rm *Manager) save() error {
	return rm.appData.Merge(rm.data)
}

func (rm *Manager) GetRules() []Rule {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Р’РѕР·РІСЂР°С‰Р°РµРј РєРѕРїРёСЋ, С‡С‚РѕР±С‹ РІС‹Р·С‹РІР°СЋС‰РёР№ РєРѕРґ РЅРµ С‡РёС‚Р°Р» СЃР»Р°Р№СЃ, РєРѕС‚РѕСЂС‹Р№ РјРµРЅСЏСЋС‚ РїРѕРґ Р±Р»РѕРєРёСЂРѕРІРєРѕР№.
	return slices.Clone(rm.data.Rules)
}

// AddRule РґРѕР±Р°РІР»СЏРµС‚ РїСЂР°РІРёР»Рѕ. Р—РЅР°С‡РµРЅРёРµ РЅРѕСЂРјР°Р»РёР·СѓРµС‚СЃСЏ (СЃРј. normalizeRule), РїРµСЂРµСЃРµС‡РµРЅРёРµ СЃ СЂСѓС‡РЅС‹РјРё РїСЂР°РІРёР»Р°РјРё Р»СЋР±РѕР№
// РіСЂСѓРїРїС‹ Р·Р°РїСЂРµС‰РµРЅРѕ. Р’РѕР·РІСЂР°С‰Р°РµС‚ РґРѕР±Р°РІР»РµРЅРЅРѕРµ РїСЂР°РІРёР»Рѕ Рё РїСЂРµРґСѓРїСЂРµР¶РґРµРЅРёСЏ Рѕ РїРµСЂРµСЃРµС‡РµРЅРёРё СЃ URL-РёСЃС‚РѕС‡РЅРёРєР°РјРё РіСЂСѓРїРї,
// РєРѕС‚РѕСЂС‹Рµ СЃС‚РѕСЏС‚ РІС‹С€Рµ: РґР»СЏ С‚Р°РєРёС… Р·РЅР°С‡РµРЅРёР№ СЃСЂР°Р±РѕС‚Р°СЋС‚ РѕРЅРё.
func (rm *Manager) AddRule(rule Rule) (Rule, []string, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// РџСЂРѕРІРµСЂСЏРµРј, С‡С‚Рѕ РіСЂСѓРїРїР° СЃСѓС‰РµСЃС‚РІСѓРµС‚.
	if !rm.groupExists(rule.Group) {
		return Rule{}, nil, fmt.Errorf("РіСЂСѓРїРїР° %s РЅРµ СЃСѓС‰РµСЃС‚РІСѓРµС‚", rule.Group)
	}

	if err := normalizeRule(&rule); err != nil {
		return Rule{}, nil, err
	}

	if err := checkConflicts(rule, rm.data.Rules); err != nil {
		return Rule{}, nil, err
	}

	rule.Applied = false
	rule.Deleted = false
	rule.CreatedAt = time.Now()
	rm.data.Rules = append(rm.data.Rules, rule)

	if err := rm.save(); err != nil {
		return Rule{}, nil, err
	}

	return rule, rm.newSourceShadows(rule.Group).check(rule), nil
}

// BulkAddResult СЃРѕРґРµСЂР¶РёС‚ СЂРµР·СѓР»СЊС‚Р°С‚С‹ РјР°СЃСЃРѕРІРѕРіРѕ РґРѕР±Р°РІР»РµРЅРёСЏ РїСЂР°РІРёР».
type BulkAddResult struct {
	Success      int              `json:"success"`
	Failed       int              `json:"failed"`
	Total        int              `json:"total"`
	FailedValues []BulkAddFailure `json:"failed_values,omitempty"`
	AddedRules   []Rule           `json:"added_rules,omitempty"`

	// Warnings вЂ” РґРѕР±Р°РІР»РµРЅРЅС‹Рµ Р·РЅР°С‡РµРЅРёСЏ, РєРѕС‚РѕСЂС‹Рµ РІС…РѕРґСЏС‚ РІ URL-РёСЃС‚РѕС‡РЅРёРєРё РіСЂСѓРїРї РІС‹С€Рµ.
	Warnings []BulkAddFailure `json:"warnings,omitempty"`
}

// BulkAddFailure СЃРѕРґРµСЂР¶РёС‚ РёРЅС„РѕСЂРјР°С†РёСЋ РѕР± РѕС€РёР±РєРµ РїСЂРё РґРѕР±Р°РІР»РµРЅРёРё РїСЂР°РІРёР»Р°.
type BulkAddFailure struct {
	Value string `json:"value"`
	Error string `json:"error"`
}

// AddRuleBulk РґРѕР±Р°РІР»СЏРµС‚ РЅРµСЃРєРѕР»СЊРєРѕ РїСЂР°РІРёР» РѕРґРЅРѕРІСЂРµРјРµРЅРЅРѕ.
func (rm *Manager) AddRuleBulk(
	ruleType, values, description, group string,
) (*BulkAddResult, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.groupExists(group) {
		return nil, fmt.Errorf("РіСЂСѓРїРїР° %s РЅРµ СЃСѓС‰РµСЃС‚РІСѓРµС‚", group)
	}

	lines := strings.Split(values, "\n")
	shadows := rm.newSourceShadows(group)
	result := &BulkAddResult{
		Total:        0,
		Success:      0,
		Failed:       0,
		FailedValues: []BulkAddFailure{},
		AddedRules:   []Rule{},
		Warnings:     []BulkAddFailure{},
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		result.Total++

		// ID РІРёРґР° В«РІСЂРµРјСЏ-РЅРѕРјРµСЂВ» СЃРѕРІРїР°РґР°Р»Рё Р±С‹ Сѓ РґРІСѓС… РјР°СЃСЃРѕРІС‹С… РґРѕР±Р°РІР»РµРЅРёР№ РІ РѕРґРЅСѓ СЃРµРєСѓРЅРґСѓ.
		rule := Rule{
			ID:          uuid.NewString(),
			Type:        ruleType,
			Value:       line,
			Description: description,
			Group:       group,
			Applied:     false,
			CreatedAt:   time.Now(),
		}

		// Р”РѕР±Р°РІР»РµРЅРЅС‹Рµ Р·РЅР°С‡РµРЅРёСЏ РїРѕРїР°РґР°СЋС‚ РІ rm.data.Rules, РїРѕСЌС‚РѕРјСѓ СЃР»РµРґСѓСЋС‰РёРµ РїСЂРѕРІРµСЂСЏСЋС‚СЃСЏ Рё СЃ РЅРёРјРё.
		err := normalizeRule(&rule)
		if err == nil {
			err = checkConflicts(rule, rm.data.Rules)
		}

		if err != nil {
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

		for _, warning := range shadows.check(rule) {
			result.Warnings = append(result.Warnings, BulkAddFailure{
				Value: rule.Value,
				Error: warning,
			})
		}
	}

	if result.Success > 0 {
		if err := rm.save(); err != nil {
			return nil, fmt.Errorf("cannot save rules: %w", err)
		}
	}

	return result, nil
}

// EditRule РѕР±РЅРѕРІР»СЏРµС‚ РїР°СЂР°РјРµС‚СЂС‹ РїСЂР°РІРёР»Р°.
func (rm *Manager) EditRule(id string, description string, group string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// РџСЂРѕРІРµСЂСЏРµРј, С‡С‚Рѕ РіСЂСѓРїРїР° СЃСѓС‰РµСЃС‚РІСѓРµС‚.
	if !rm.groupExists(group) {
		return fmt.Errorf("РіСЂСѓРїРїР° %s РЅРµ СЃСѓС‰РµСЃС‚РІСѓРµС‚", group)
	}

	// РќР°С…РѕРґРёРј РїСЂР°РІРёР»Рѕ.
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
		return fmt.Errorf("РїСЂР°РІРёР»Рѕ СЃ ID %s РЅРµ РЅР°Р№РґРµРЅРѕ", id)
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

// GetRuleSetByGroup РІРѕР·РІСЂР°С‰Р°РµС‚ РЅР°Р±РѕСЂ РїСЂР°РІРёР» РІРёРґР° kind РґР»СЏ СѓРєР°Р·Р°РЅРЅРѕР№ РіСЂСѓРїРїС‹.
func (rm *Manager) GetRuleSetByGroup(groupName string, kind RuleSetKind) (SingBoxRuleSet, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.getRuleSetLocked(kind, groupName)
}

// GetBypassRuleSet РІРѕР·РІСЂР°С‰Р°РµС‚ РЅР°Р±РѕСЂ РїСЂР°РІРёР» СЃРёСЃС‚РµРјРЅРѕР№ РіСЂСѓРїРїС‹ bypass (РјРёРјРѕ С‚СѓРЅРЅРµР»СЏ).
func (rm *Manager) GetBypassRuleSet() (SingBoxRuleSet, error) {
	return rm.GetRuleSetByGroup(BypassGroupName, RuleSetKindAll)
}

// getRuleSetLocked СЃРѕР±РёСЂР°РµС‚ РЅР°Р±РѕСЂ РІРёРґР° kind РёР· РїСЂРёРјРµРЅРµРЅРЅС‹С… РїСЂР°РІРёР» Рё РёСЃС‚РѕС‡РЅРёРєРѕРІ РіСЂСѓРїРїС‹ groupName
// (Р±РµР· Р±Р»РѕРєРёСЂРѕРІРєРё). Р•СЃР»Рё РєР°РєРѕР№-С‚Рѕ РёР· РёСЃС‚РѕС‡РЅРёРєРѕРІ РµС‰Рµ РЅРµ Р·Р°РіСЂСѓР¶РµРЅ РїРѕСЃР»Рµ СЃС‚Р°СЂС‚Р°, РІРѕР·РІСЂР°С‰Р°РµС‚ ErrRuleSetNotReady.
func (rm *Manager) getRuleSetLocked(kind RuleSetKind, groupName string) (SingBoxRuleSet, error) {
	rules := make([]map[string]any, 0, 1)

	var (
		domains        []string
		domainSuffixes []string
		ipCidrs        []string
	)

	// РљР°СЂС‚С‹ СЂСѓС‡РЅС‹С… РїСЂР°РІРёР» РґР»СЏ РѕС‚СЃРµС‡РµРЅРёСЏ РєРѕРЅС„Р»РёРєС‚СѓСЋС‰РёС… Р·РЅР°С‡РµРЅРёР№ РёР· РёСЃС‚РѕС‡РЅРёРєРѕРІ.
	manualDomains := make(map[string]bool)
	manualSuffixes := make(map[string]bool)

	// РџСЂР°РІРёР»Р°, РїРѕРјРµС‡РµРЅРЅС‹Рµ РЅР° СѓРґР°Р»РµРЅРёРµ, РЅРѕ РїСЂРёРјРµРЅРµРЅРЅС‹Рµ, РѕСЃС‚Р°СЋС‚СЃСЏ РІ РЅР°Р±РѕСЂРµ РґРѕ РїСЂРёРјРµРЅРµРЅРёСЏ РёР·РјРµРЅРµРЅРёР№.
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

	// РСЃС‚РѕС‡РЅРёРєРё, РµС‰Рµ РЅРµ Р·Р°РіСЂСѓР¶РµРЅРЅС‹Рµ РїРѕСЃР»Рµ СЃС‚Р°СЂС‚Р°.
	var notLoaded []string

	// РСЃС‚РѕС‡РЅРёРєРё, РїРѕРјРµС‡РµРЅРЅС‹Рµ РЅР° СѓРґР°Р»РµРЅРёРµ, РЅРѕ РїСЂРёРјРµРЅРµРЅРЅС‹Рµ, С‚Р°РєР¶Рµ РѕСЃС‚Р°СЋС‚СЃСЏ РІ РЅР°Р±РѕСЂРµ.
	rm.urlRulesMu.RLock()

	for _, source := range rm.data.URLSources {
		if !source.Applied || source.Group != groupName {
			continue
		}

		ruleSet, ok := rm.urlRules[source.ID]

		// РСЃС‚РѕС‡РЅРёРє Р±РµР· РєСЌС€Р°, РєРѕС‚РѕСЂС‹Р№ РЅРµ Р·Р°РіСЂСѓР·РёР»СЃСЏ Рё РїРѕСЃР»Рµ РїРѕРїС‹С‚РєРё, РЅРµ РґРµСЂР¶РёС‚ РІСЃСЋ РіСЂСѓРїРїСѓ: РїСЂРµР¶РґРµ
		// РІ РЅР°Р±РѕСЂРµ РµРіРѕ С‚РѕР¶Рµ РЅРµ Р±С‹Р»Рѕ, Р° Р±РµР· РЅР°Р±РѕСЂР° sing-box РјРѕР¶РµС‚ РЅРµ Р·Р°РїСѓСЃС‚РёС‚СЊСЃСЏ.
		if !ok && !rm.attempted[source.ID] {
			notLoaded = append(notLoaded, source.Description)

			continue
		}

		if !ok {
			continue
		}

		// IP/CIDR РґРѕР±Р°РІР»СЏРµРј Р±РµР· РїСЂРѕРІРµСЂРєРё РєРѕРЅС„Р»РёРєС‚РѕРІ.
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

	// Р’СЃРµ Р·РЅР°С‡РµРЅРёСЏ СЃРѕР±РёСЂР°СЋС‚СЃСЏ РІ РѕРґРЅРѕ РїСЂР°РІРёР»Рѕ.
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

// hasConflictWithManualRules РїСЂРѕРІРµСЂСЏРµС‚, РєРѕРЅС„Р»РёРєС‚СѓРµС‚ Р»Рё РїСЂР°РІРёР»Рѕ СЃ СЂСѓС‡РЅС‹РјРё РїСЂР°РІРёР»Р°РјРё.
func hasConflictWithManualRules(value, ruleType string, manualDomains, manualSuffixes map[string]bool) bool {
	if ruleType == "domain" {
		// РџСЂРѕРІРµСЂСЏРµРј, РµСЃС‚СЊ Р»Рё СЃСѓС„С„РёРєСЃ, РєРѕС‚РѕСЂС‹Р№ РїРѕРєСЂС‹РІР°РµС‚ СЌС‚РѕС‚ РґРѕРјРµРЅ.
		for suffix := range manualSuffixes {
			if isDomainMatchesSuffix(value, suffix) {
				return true
			}
		}
	}

	if ruleType == "domain_suffix" {
		// РџСЂРѕРІРµСЂСЏРµРј, РµСЃС‚СЊ Р»Рё РґРѕРјРµРЅ, РєРѕС‚РѕСЂС‹Р№ РєРѕРЅС„Р»РёРєС‚СѓРµС‚ СЃ СЌС‚РёРј СЃСѓС„С„РёРєСЃРѕРј.
		for domain := range manualDomains {
			if isDomainMatchesSuffix(domain, value) {
				return true
			}
		}

		// РџСЂРѕРІРµСЂСЏРµРј, РµСЃС‚СЊ Р»Рё СЃСѓС„С„РёРєСЃ, РєРѕС‚РѕСЂС‹Р№ РєРѕРЅС„Р»РёРєС‚СѓРµС‚ СЃ СЌС‚РёРј СЃСѓС„С„РёРєСЃРѕРј.
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

// GetGroups РІРѕР·РІСЂР°С‰Р°РµС‚ РїРѕР»СЊР·РѕРІР°С‚РµР»СЊСЃРєРёРµ РіСЂСѓРїРїС‹ (Р±РµР· СЃРёСЃС‚РµРјРЅС‹С…) РІ РїРѕСЂСЏРґРєРµ РёС… СЃРѕР·РґР°РЅРёСЏ.
func (rm *Manager) GetGroups() []Group {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Р’РѕР·РІСЂР°С‰Р°РµРј РєРѕРїРёСЋ, С‡С‚РѕР±С‹ РІС‹Р·С‹РІР°СЋС‰РёР№ РєРѕРґ РЅРµ С‡РёС‚Р°Р» СЃР»Р°Р№СЃ, РєРѕС‚РѕСЂС‹Р№ РјРµРЅСЏСЋС‚ РїРѕРґ Р±Р»РѕРєРёСЂРѕРІРєРѕР№.
	return slices.Clone(rm.data.Groups)
}

// GetAllGroups РІРѕР·РІСЂР°С‰Р°РµС‚ СЃРёСЃС‚РµРјРЅС‹Рµ РіСЂСѓРїРїС‹, Р° Р·Р° РЅРёРјРё РїРѕР»СЊР·РѕРІР°С‚РµР»СЊСЃРєРёРµ.
func (rm *Manager) GetAllGroups() []Group {
	return append(SystemGroups(), rm.GetGroups()...)
}

// GroupsByDNSServer РІРѕР·РІСЂР°С‰Р°РµС‚ РёРјРµРЅР° РїРѕР»СЊР·РѕРІР°С‚РµР»СЊСЃРєРёС… РіСЂСѓРїРї, Сѓ РєРѕС‚РѕСЂС‹С… DNS-СЃРµСЂРІРµСЂ СЂР°РІРµРЅ dnsServer.
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

// AddGroup РґРѕР±Р°РІР»СЏРµС‚ РЅРѕРІСѓСЋ РіСЂСѓРїРїСѓ.
func (rm *Manager) AddGroup(group Group) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	group.Name = strings.TrimSpace(group.Name)

	if !groupNameRe.MatchString(group.Name) {
		return fmt.Errorf("РЅРµРєРѕСЂСЂРµРєС‚РЅРѕРµ РёРјСЏ РіСЂСѓРїРїС‹ %q: РґРѕРїСѓСЃС‚РёРјС‹ Р»Р°С‚РёРЅРёС†Р°, С†РёС„СЂС‹, РґРµС„РёСЃ Рё РїРѕРґС‡РµСЂРєРёРІР°РЅРёРµ", group.Name)
	}

	if IsSystemGroup(group.Name) {
		return fmt.Errorf("РёРјСЏ РіСЂСѓРїРїС‹ %s Р·Р°СЂРµР·РµСЂРІРёСЂРѕРІР°РЅРѕ", group.Name)
	}

	// РџСЂРѕРІРµСЂСЏРµРј, С‡С‚Рѕ РіСЂСѓРїРїР° СЃ С‚Р°РєРёРј РёРјРµРЅРµРј РЅРµ СЃСѓС‰РµСЃС‚РІСѓРµС‚.
	for _, g := range rm.data.Groups {
		if g.Name == group.Name {
			return fmt.Errorf("РіСЂСѓРїРїР° СЃ РёРјРµРЅРµРј %s СѓР¶Рµ СЃСѓС‰РµСЃС‚РІСѓРµС‚", group.Name)
		}
	}

	group.System = false
	group.CreatedAt = time.Now()
	rm.data.Groups = append(rm.data.Groups, group)

	return rm.save()
}

// EditGroup РѕР±РЅРѕРІР»СЏРµС‚ РїР°СЂР°РјРµС‚СЂС‹ РіСЂСѓРїРїС‹.
func (rm *Manager) EditGroup(name, description, defaultOutbound, dnsServer string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if IsSystemGroup(name) {
		return fmt.Errorf("СЃРёСЃС‚РµРјРЅСѓСЋ РіСЂСѓРїРїСѓ %s РЅРµР»СЊР·СЏ РёР·РјРµРЅРёС‚СЊ", name)
	}

	// РќР°С…РѕРґРёРј РіСЂСѓРїРїСѓ.
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
		return fmt.Errorf("РіСЂСѓРїРїР° %s РЅРµ РЅР°Р№РґРµРЅР°", name)
	}

	return rm.save()
}

// DeleteGroup СѓРґР°Р»СЏРµС‚ РіСЂСѓРїРїСѓ.
func (rm *Manager) DeleteGroup(name string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if IsSystemGroup(name) {
		return fmt.Errorf("СЃРёСЃС‚РµРјРЅСѓСЋ РіСЂСѓРїРїСѓ %s РЅРµР»СЊР·СЏ СѓРґР°Р»РёС‚СЊ", name)
	}

	// РЈРґР°Р»РёС‚СЊ РјРѕР¶РЅРѕ С‚РѕР»СЊРєРѕ РїСѓСЃС‚СѓСЋ РіСЂСѓРїРїСѓ.
	for _, rule := range rm.data.Rules {
		if rule.Group == name && !rule.Deleted {
			return fmt.Errorf("РІ РіСЂСѓРїРїРµ %s РµСЃС‚СЊ РїСЂР°РІРёР»Р°, СѓРґР°Р»РёС‚Рµ РёС… СЃРЅР°С‡Р°Р»Р°", name)
		}
	}

	for _, source := range rm.data.URLSources {
		if source.Group == name && !source.Deleted {
			return fmt.Errorf("РІ РіСЂСѓРїРїРµ %s РµСЃС‚СЊ РёСЃС‚РѕС‡РЅРёРєРё, СѓРґР°Р»РёС‚Рµ РёС… СЃРЅР°С‡Р°Р»Р°", name)
		}
	}

	// РЈРґР°Р»СЏРµРј РіСЂСѓРїРїСѓ.
	newGroups := []Group{}

	for _, g := range rm.data.Groups {
		if g.Name != name {
			newGroups = append(newGroups, g)
		}
	}

	rm.data.Groups = newGroups

	return rm.save()
}

// groupExists РїСЂРѕРІРµСЂСЏРµС‚, СЃСѓС‰РµСЃС‚РІСѓРµС‚ Р»Рё РіСЂСѓРїРїР° (РїРѕР»СЊР·РѕРІР°С‚РµР»СЊСЃРєР°СЏ РёР»Рё СЃРёСЃС‚РµРјРЅР°СЏ) СЃ Р·Р°РґР°РЅРЅС‹Рј РёРјРµРЅРµРј.
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

// isDomainMatchesSuffix РїСЂРѕРІРµСЂСЏРµС‚, СЏРІР»СЏРµС‚СЃСЏ Р»Рё РґРѕРјРµРЅ РїРѕРґРґРѕРјРµРЅРѕРј СЃСѓС„С„РёРєСЃР°.
func isDomainMatchesSuffix(domain, suffix string) bool {
	// example.com СЃРѕРІРїР°РґР°РµС‚ СЃ СЃСѓС„С„РёРєСЃРѕРј example.com.
	if domain == suffix {
		return true
	}

	// test.example.com СЃРѕРІРїР°РґР°РµС‚ СЃ СЃСѓС„С„РёРєСЃРѕРј example.com.
	if strings.HasSuffix(domain, "."+suffix) {
		return true
	}

	return false
}
