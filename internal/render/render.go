// Package render собирает конфиг sing-box из данных app.json и вшитого базового шаблона.
// Рендер — чистая функция: одинаковые данные всегда дают одинаковый конфиг.
package render

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/settings"
)

const (
	// RuleSetTagPrefix — префикс тегов rule-set-ов, которые sing-box забирает у конфигуратора.
	RuleSetTagPrefix = "configurer"

	// ipRuleSetTagSuffix — суффикс тега rule-set-а с IP/CIDR группы. Символ @ недопустим в именах групп.
	ipRuleSetTagSuffix = "@ip"

	// HostsServerTag — тег DNS-сервера с DNS-записями конфигуратора.
	HostsServerTag = RuleSetTagPrefix + "-hosts"

	// httpClientTag — HTTP-клиент, через который sing-box загружает rule-set-ы.
	httpClientTag = "default_http_client"

	// ruleSetUpdateInterval — как часто sing-box проверяет rule-set-ы (ответ 304, если набор не изменился).
	ruleSetUpdateInterval = "30s"
)

// baseTemplate — постоянная часть конфига: tun и dns inbound-ы, встроенные outbound-ы, служебные
// маршруты, HTTP-клиент и experimental.
//
//go:embed base.json
var baseTemplate []byte

// Input — данные, из которых собирается конфиг.
type Input struct {
	// Groups — пользовательские группы в порядке создания (без системных).
	Groups     []rules.Group
	DNS        dnsconfig.Data
	DNSRecords []dnsrecords.Record

	// Outbounds — outbound-ы и endpoint-ы, добавленные вручную.
	Outbounds []map[string]any

	// Subscriptions — outbound-ы профилей подписок Happ и Amnezia.
	Subscriptions []outbound.Subscription
	URLTests      []outbound.URLTest
	Mixed         []inbounds.Mixed
	Settings      settings.Settings

	// RuleSetBaseURL — адрес конфигуратора, по которому sing-box забирает rule-set-ы (без / в конце).
	RuleSetBaseURL string

	// SourcesProxy — служебный inbound для загрузки URL-источников через outbound-ы.
	SourcesProxy SourcesProxy
}

// SourcesProxyTag — тег служебного mixed-inbound-а, через который конфигуратор загружает URL-источники с detour.
const SourcesProxyTag = "configurer-sources"

// SourcesProxy описывает служебный inbound: логин пользователя — тег outbound-а, пароль общий
// (Settings.SourcesProxy.Password). Inbound рендерится, только если есть источники с detour.
type SourcesProxy struct {
	Listen string
	Port   int

	// Detours — outbound-ы, через которые загружаются источники.
	Detours []string
}

// Result — отрендеренный конфиг и предупреждения о пропущенных элементах.
type Result struct {
	Config   map[string]any
	Warnings []string
}

// renderer хранит состояние одного рендера.
type renderer struct {
	in       Input
	warnings []string

	// proxyOutbounds и endpoints — outbound-ы и endpoint-ы с уникальными тегами.
	proxyOutbounds []any
	endpoints      []any

	// proxyTags — теги всех прокси в порядке объявления, candidates — они же с источниками для urltest-ов.
	proxyTags  []string
	candidates []outbound.Candidate

	// outboundTags — теги всех outbound-ов итогового конфига, включая selector-ы групп.
	outboundTags []string

	// sourceDetours — outbound-ы загрузки URL-источников, которые есть в конфиге.
	sourceDetours []string
}

// Render собирает конфиг sing-box.
func Render(in Input) (*Result, error) {
	var config map[string]any

	if err := json.Unmarshal(baseTemplate, &config); err != nil {
		return nil, fmt.Errorf("cannot parse base template: %w", err)
	}

	r := &renderer{
		in:             in,
		warnings:       []string{},
		proxyOutbounds: []any{},
		endpoints:      []any{},
		proxyTags:      []string{},
		candidates:     []outbound.Candidate{},
		outboundTags:   []string{},
		sourceDetours:  []string{},
	}

	r.collectProxies()

	if in.Settings.LogLevel != "" {
		config["log"].(map[string]any)["level"] = in.Settings.LogLevel
	}

	r.renderOutbounds(config)
	r.sourceDetours = r.collectSourceDetours()
	r.renderInbounds(config)
	r.renderRoute(config)
	r.renderDNS(config)
	r.renderExperimental(config)

	return &Result{
		Config:   config,
		Warnings: r.warnings,
	}, nil
}

// Candidates возвращает outbound-ы, из которых urltest-ы подбирают участников, — в том виде, в котором
// они попадут в конфиг (без пропущенных и повторяющихся).
func Candidates(in Input) []outbound.Candidate {
	r := &renderer{
		in:             in,
		warnings:       []string{},
		proxyOutbounds: []any{},
		endpoints:      []any{},
		proxyTags:      []string{},
		candidates:     []outbound.Candidate{},
		outboundTags:   []string{},
		sourceDetours:  []string{},
	}

	r.collectProxies()

	return r.candidates
}

// warn добавляет предупреждение.
func (r *renderer) warn(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// collectProxies собирает ручные outbound-ы и outbound-ы подписок. Элементы без тега, с зарезервированным
// тегом, тегом urltest-а или повторяющимся тегом пропускаются с предупреждением.
func (r *renderer) collectProxies() {
	seen := map[string]string{}

	for _, urlTest := range r.in.URLTests {
		seen[urlTest.Tag] = "urltest"
	}

	add := func(label string, candidate outbound.Candidate, config map[string]any) {
		tag := jsonmap.String(config, "tag")

		switch {
		case tag == "":
			r.warn("%s: outbound без тега пропущен", label)

			return

		case outbound.IsReservedTag(tag):
			r.warn("%s: outbound %s пропущен — тег зарезервирован", label, tag)

			return
		}

		if previous, ok := seen[tag]; ok {
			r.warn("%s: outbound %s пропущен — тег уже занят (%s)", label, tag, previous)

			return
		}

		seen[tag] = label
		r.proxyTags = append(r.proxyTags, tag)

		candidate.Tag = tag
		candidate.Type = jsonmap.String(config, "type")
		r.candidates = append(r.candidates, candidate)

		if outbound.IsEndpoint(config) {
			r.endpoints = append(r.endpoints, jsonmap.Clone(config))

			return
		}

		r.proxyOutbounds = append(r.proxyOutbounds, jsonmap.Clone(config))
	}

	for _, config := range r.in.Outbounds {
		add("Outbounds", outbound.Candidate{
			Tag:       "",
			Type:      "",
			Source:    outbound.SourceManual,
			ProfileID: "",
		}, config)
	}

	for _, subscription := range r.in.Subscriptions {
		for _, config := range subscription.Outbounds {
			add(subscriptionLabel(subscription.Source, subscription.ProfileName), outbound.Candidate{
				Tag:       "",
				Type:      "",
				Source:    subscription.Source,
				ProfileID: subscription.ProfileID,
			}, config)
		}
	}
}

// renderURLTests собирает urltest-ы и возвращает их объекты. urltest без участников sing-box не примет,
// поэтому такой urltest пропускается: группы, выбравшие его, получат block.
func (r *renderer) renderURLTests() []any {
	result := make([]any, 0, len(r.in.URLTests))

	for _, urlTest := range r.in.URLTests {
		for _, source := range urlTest.Sources {
			if source.ProfileID != "" && !r.hasSubscription(source) {
				r.warn("urltest %s: профиль %s источника %s не найден", urlTest.Tag, source.ProfileID, source.Kind)
			}
		}

		members, err := urlTest.Resolve(r.candidates)
		if err != nil {
			r.warn("urltest %s пропущен: %v", urlTest.Tag, err)

			continue
		}

		for _, member := range members {
			if member.State == outbound.MemberMissing {
				r.warn("urltest %s: outbound %s не найден", urlTest.Tag, member.Tag)
			}
		}

		included := outbound.IncludedTags(members)

		if len(included) == 0 {
			r.warn("urltest %s пропущен — в нем нет outbound-ов", urlTest.Tag)

			continue
		}

		result = append(result, urlTest.Config(included))
	}

	return result
}

// hasSubscription проверяет, что профиль источника source есть среди подписок.
func (r *renderer) hasSubscription(source outbound.URLTestSource) bool {
	return slices.ContainsFunc(r.in.Subscriptions, func(subscription outbound.Subscription) bool {
		return subscription.Source == source.Kind && subscription.ProfileID == source.ProfileID
	})
}

// renderOutbounds собирает outbounds: прокси, urltest-ы, встроенные direct и block, selector-ы групп.
func (r *renderer) renderOutbounds(config map[string]any) {
	builtin, _ := config["outbounds"].([]any)
	urlTests := r.renderURLTests()

	outbounds := slices.Clone(r.proxyOutbounds)
	outbounds = append(outbounds, urlTests...)
	outbounds = append(outbounds, builtin...)

	members := make([]string, 0, len(urlTests)+len(r.proxyTags)+2)

	for _, urlTest := range urlTests {
		members = append(members, jsonmap.String(urlTest.(map[string]any), "tag"))
	}

	members = append(members, r.proxyTags...)
	members = append(members, outbound.DirectTag, outbound.BlockTag)

	for _, group := range r.in.Groups {
		defaultOutbound := group.DefaultOutbound
		if defaultOutbound == "" {
			defaultOutbound = outbound.DirectTag
		}

		// Без выбранного outbound-а трафик группы блокируется, а не уходит мимо VPN.
		if !slices.Contains(members, defaultOutbound) {
			r.warn("группа %s: outbound %s не найден, по умолчанию выбран block", group.Name, defaultOutbound)

			defaultOutbound = outbound.BlockTag
		}

		outbounds = append(outbounds, map[string]any{
			"type":                        "selector",
			"tag":                         SelectorTag(group.Name),
			"outbounds":                   toAnySlice(members),
			"default":                     defaultOutbound,
			"interrupt_exist_connections": true,
		})
	}

	r.outboundTags = slices.Clone(members)

	for _, group := range r.in.Groups {
		r.outboundTags = append(r.outboundTags, SelectorTag(group.Name))
	}

	config["outbounds"] = outbounds

	if len(r.endpoints) > 0 {
		config["endpoints"] = r.endpoints
	}
}

// renderInbounds вставляет mixed-inbound-ы после tun-in.
func (r *renderer) renderInbounds(config map[string]any) {
	base, _ := config["inbounds"].([]any)
	result := make([]any, 0, len(base)+len(r.in.Mixed))

	for _, item := range base {
		result = append(result, item)

		if jsonmap.String(item.(map[string]any), "tag") != inbounds.TunTag {
			continue
		}

		for _, mixed := range r.in.Mixed {
			result = append(result, mixed.Config())
		}
	}

	if len(r.sourceDetours) > 0 {
		result = append(result, r.sourcesProxyInbound(r.sourceDetours))
	}

	config["inbounds"] = result
}

// renderRoute собирает rule-set-ы и маршрутные правила групп.
func (r *renderer) renderRoute(config map[string]any) {
	route := config["route"].(map[string]any)
	baseRules, _ := route["rules"].([]any)

	ruleSets := []any{
		r.remoteRuleSet(RuleSetTag(rules.BypassGroupName), "/api/ruleset/bypass"),
		r.remoteRuleSet(RuleSetTag(rules.BlockGroupName), groupRuleSetPath("domain", rules.BlockGroupName)),
		r.remoteRuleSet(IPRuleSetTag(rules.BlockGroupName), groupRuleSetPath("ip", rules.BlockGroupName)),
	}

	routeRules := make([]any, 0, len(baseRules)+len(r.in.Groups)+2)

	// Загрузка источников через outbound: правила первыми, до bypass и sniff — они для служебного inbound-а не нужны.
	for _, detour := range r.sourceDetours {
		routeRules = append(routeRules, map[string]any{
			"inbound":   SourcesProxyTag,
			"auth_user": []any{detour},
			"outbound":  detour,
		})
	}

	for _, item := range baseRules {
		rule := item.(map[string]any)

		// Mixed-прокси получает домены: резолвим их до правил, иначе IP-правила групп не сработают.
		if jsonmap.Bool(rule, "ip_is_private") && len(r.in.Mixed) > 0 {
			routeRules = append(routeRules, map[string]any{
				"action":  "resolve",
				"inbound": r.mixedTags(),
			})
		}

		routeRules = append(routeRules, rule)
	}

	routeRules = append(routeRules, map[string]any{
		"action": "reject",
		"rule_set": []any{
			RuleSetTag(rules.BlockGroupName),
			IPRuleSetTag(rules.BlockGroupName),
		},
	})

	for _, group := range r.in.Groups {
		ruleSets = append(ruleSets,
			r.remoteRuleSet(RuleSetTag(group.Name), groupRuleSetPath("domain", group.Name)),
			r.remoteRuleSet(IPRuleSetTag(group.Name), groupRuleSetPath("ip", group.Name)),
		)

		routeRules = append(routeRules, map[string]any{
			"outbound": SelectorTag(group.Name),
			"rule_set": []any{
				RuleSetTag(group.Name),
				IPRuleSetTag(group.Name),
			},
		})
	}

	route["rule_set"] = ruleSets
	route["rules"] = routeRules

	if resolver := r.in.DNS.Settings.DefaultDomainResolver; resolver != "" {
		if r.hasDNSServer(resolver) {
			route["default_domain_resolver"] = resolver
		} else {
			r.warn("DNS: сервер %s для default_domain_resolver не найден", resolver)
		}
	}
}

// mixedTags возвращает теги mixed-inbound-ов: строкой, если он один, иначе списком.
func (r *renderer) mixedTags() any {
	if len(r.in.Mixed) == 1 {
		return r.in.Mixed[0].Tag
	}

	tags := make([]any, 0, len(r.in.Mixed))

	for _, mixed := range r.in.Mixed {
		tags = append(tags, mixed.Tag)
	}

	return tags
}

// renderDNS собирает секцию dns: серверы, DNS-записи, правила групп и пользовательские правила.
func (r *renderer) renderDNS(config map[string]any) {
	dns := config["dns"].(map[string]any)

	servers := make([]any, 0, len(r.in.DNS.Servers)+1)

	for _, server := range r.in.DNS.Servers {
		servers = append(servers, server.Config())
	}

	dnsRules := make([]any, 0)

	if len(r.in.DNSRecords) > 0 {
		server, rule := hostsEntries(r.in.DNSRecords)

		servers = append(servers, server)
		dnsRules = append(dnsRules, rule)
	}

	groupRules := make([]any, 0)

	for _, group := range r.in.Groups {
		if group.DNSServer == "" {
			continue
		}

		if !r.hasDNSServer(group.DNSServer) {
			r.warn("группа %s: DNS-сервер %s не найден, DNS-правило группы пропущено", group.Name, group.DNSServer)

			continue
		}

		groupRules = append(groupRules, map[string]any{
			"rule_set": RuleSetTag(group.Name),
			"server":   group.DNSServer,
		})
	}

	// Без HTTPS-записей клиенты не получают ECH-ключи и отправляют настоящий SNI: иначе sniff видит
	// только cloudflare-ech.com, и соединения уходят мимо правил групп.
	if len(groupRules) > 0 {
		dnsRules = append(dnsRules, map[string]any{
			"action":     "predefined",
			"query_type": []any{"HTTPS"},
			"rcode":      "NOERROR",
		})
	}

	dnsRules = append(dnsRules, groupRules...)

	for _, rule := range r.in.DNS.Rules {
		dnsRules = append(dnsRules, jsonmap.Clone(rule))
	}

	dns["servers"] = servers
	dns["rules"] = dnsRules

	dnsSettings := r.in.DNS.Settings

	if dnsSettings.Final != "" && !r.hasDNSServer(dnsSettings.Final) {
		r.warn("DNS: сервер %s для final не найден", dnsSettings.Final)

		dnsSettings.Final = ""
	}

	dnsSettings.Apply(dns)
}

// renderExperimental дописывает в clash_api секрет и CORS-origin-ы.
func (r *renderer) renderExperimental(config map[string]any) {
	experimental := config["experimental"].(map[string]any)
	clashAPI := experimental["clash_api"].(map[string]any)

	if secret := r.in.Settings.ClashAPI.Secret; secret != "" {
		clashAPI["secret"] = secret
	}

	if origins := r.in.Settings.ClashAPI.AllowOrigins; len(origins) > 0 {
		clashAPI["access_control_allow_origin"] = toAnySlice(origins)
	}
}

// hasDNSServer проверяет, что DNS-сервер с тегом tag объявлен.
func (r *renderer) hasDNSServer(tag string) bool {
	return slices.ContainsFunc(r.in.DNS.Servers, func(server dnsconfig.Server) bool {
		return server.Tag == tag
	})
}

// remoteRuleSet возвращает описание rule-set-а, который sing-box забирает у конфигуратора по пути path.
func (r *renderer) remoteRuleSet(tag, path string) map[string]any {
	return map[string]any{
		"type":   "remote",
		"tag":    tag,
		"format": "source",
		"url":    r.in.RuleSetBaseURL + path,
		"http_client": map[string]any{
			"tag": httpClientTag,
		},
		"update_interval": ruleSetUpdateInterval,
	}
}

// hostsEntries возвращает hosts-сервер с DNS-записями и правило для их доменов.
func hostsEntries(records []dnsrecords.Record) (map[string]any, map[string]any) {
	predefined := map[string]any{}
	domains := make([]any, 0, len(records))

	for _, record := range records {
		predefined[record.Domain] = toAnySlice(record.Addresses)
		domains = append(domains, record.Domain)
	}

	server := map[string]any{
		"type":       "hosts",
		"tag":        HostsServerTag,
		"predefined": predefined,
	}

	rule := map[string]any{
		"domain": domains,
		"server": HostsServerTag,
	}

	return server, rule
}

// RuleSetTag возвращает тег rule-set-а с доменами группы.
func RuleSetTag(groupName string) string {
	return RuleSetTagPrefix + "-" + groupName
}

// IPRuleSetTag возвращает тег rule-set-а с IP/CIDR группы.
func IPRuleSetTag(groupName string) string {
	return RuleSetTag(groupName) + ipRuleSetTagSuffix
}

// ParseRuleSetTag возвращает имя группы rule-set-а конфигуратора с тегом tag и признак набора IP/CIDR.
// Для чужих rule-set-ов ok равен false.
func ParseRuleSetTag(tag string) (groupName string, ip bool, ok bool) {
	name, found := strings.CutPrefix(tag, RuleSetTagPrefix+"-")
	if !found || name == "" {
		return "", false, false
	}

	if group, isIP := strings.CutSuffix(name, ipRuleSetTagSuffix); isIP {
		return group, true, group != ""
	}

	return name, false, true
}

// SelectorTag возвращает тег selector-а группы.
func SelectorTag(groupName string) string {
	return outbound.SelectorTagPrefix + groupName
}

// groupRuleSetPath возвращает путь API rule-set-а вида kind группы.
func groupRuleSetPath(kind, groupName string) string {
	return fmt.Sprintf("/api/ruleset/%s?group=%s", kind, url.QueryEscape(groupName))
}

// toAnySlice преобразует список строк в []any.
func toAnySlice(values []string) []any {
	result := make([]any, 0, len(values))

	for _, value := range values {
		result = append(result, value)
	}

	return result
}

// subscriptionLabel возвращает подпись профиля подписки для предупреждений.
func subscriptionLabel(source, profileName string) string {
	switch source {
	case outbound.SourceHapp:
		return "Happ " + profileName

	case outbound.SourceAmnezia:
		return "Amnezia " + profileName

	default:
		return profileName
	}
}

// collectSourceDetours возвращает outbound-ы для загрузки источников, которые есть в конфиге. Для отсутствующих
// добавляется предупреждение: источники с ними не загрузятся.
func (r *renderer) collectSourceDetours() []string {
	proxy := r.in.SourcesProxy

	if len(proxy.Detours) == 0 {
		return nil
	}

	if proxy.Port <= 0 || r.in.Settings.SourcesProxy.Password == "" {
		r.warn("источники URL: не задан порт или пароль служебного inbound-а, загрузка через outbound-ы недоступна")

		return nil
	}

	detours := make([]string, 0, len(proxy.Detours))

	for _, detour := range proxy.Detours {
		if !slices.Contains(r.outboundTags, detour) {
			r.warn("источники URL: outbound %s для загрузки не найден, источники с ним не загрузятся", detour)

			continue
		}

		detours = append(detours, detour)
	}

	return detours
}

// sourcesProxyInbound возвращает служебный mixed-inbound: пользователь на каждый outbound загрузки.
func (r *renderer) sourcesProxyInbound(detours []string) map[string]any {
	users := make([]any, 0, len(detours))

	for _, detour := range detours {
		users = append(users, map[string]any{
			"username": detour,
			"password": r.in.Settings.SourcesProxy.Password,
		})
	}

	return map[string]any{
		"type":        "mixed",
		"tag":         SourcesProxyTag,
		"listen":      r.in.SourcesProxy.Listen,
		"listen_port": r.in.SourcesProxy.Port,
		"users":       users,
	}
}
