package topology

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/rules"
)

// Источники адресов домена при трассировке.
const (
	ResolvedHosts  = "hosts"
	ResolvedSystem = "system"
)

const (
	// resolveTimeout ограничивает резолв домена при трассировке.
	resolveTimeout = 3 * time.Second

	// maxChain ограничивает глубину цепочки selector-ов и urltest-ов (защита от циклов).
	maxChain = 16

	// Параметры соединения, которые подставляются при трассировке: TCP на 443 порт с TLS.
	traceNetwork  = "tcp"
	tracePort     = 443
	traceProtocol = "tls"
)

var (
	// ErrInvalidQuery — запрос трассировки не является доменом или IP.
	ErrInvalidQuery = errors.New("укажите домен или IP-адрес")

	// ErrUnknownInbound — inbound для трассировки не найден в рабочем конфиге.
	ErrUnknownInbound = errors.New("inbound не найден в рабочем конфиге")

	// domainRe — допустимое доменное имя (IDN — в punycode).
	domainRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*$`)
)

// RuleMatcher сопоставляет домен и IP с правилами групп конфигуратора.
type RuleMatcher interface {
	MatchGroup(groupName string, kind rules.RuleSetKind, domain string, ips []netip.Addr) []rules.Match
}

// Resolver возвращает адреса домена.
type Resolver func(ctx context.Context, host string) ([]netip.Addr, error)

// TraceEnv — данные для трассировки вне конфига sing-box.
type TraceEnv struct {
	Rules    RuleMatcher
	Proxies  map[string]ProxyState
	Resolver Resolver
}

// TraceRequest — что трассировать: домен или IP и inbound, через который приходит соединение.
type TraceRequest struct {
	Query   string
	Inbound string
}

// TraceStep — проверка одного правила маршрутизации.
type TraceStep struct {
	Row     string        `json:"row"`
	Label   string        `json:"label"`
	Action  string        `json:"action"`
	Matched bool          `json:"matched"`
	Reason  string        `json:"reason"`
	Matches []rules.Match `json:"matches,omitempty"`
}

// DNSTrace — правило DNS, которое обработает запрос домена.
type DNSTrace struct {
	Row     string        `json:"row"`
	Label   string        `json:"label"`
	Action  string        `json:"action"`
	Server  string        `json:"server,omitempty"`
	Reason  string        `json:"reason"`
	Matches []rules.Match `json:"matches,omitempty"`

	// Chain — outbound-ы, через которые запрос уходит к DNS-серверу (пусто, если сервер отвечает сам).
	// Implicit — detour не задан, и sing-box соединяется с сервером напрямую, мимо правил маршрутизации.
	Chain    []string `json:"chain"`
	Implicit bool     `json:"implicit,omitempty"`
}

// TraceResult — путь соединения по карте.
type TraceResult struct {
	Query  string `json:"query"`
	Domain string `json:"domain,omitempty"`

	// IPs — адреса назначения, Resolved — откуда они взяты для домена.
	IPs          []string `json:"ips"`
	Resolved     string   `json:"resolved,omitempty"`
	ResolveError string   `json:"resolve_error,omitempty"`

	Inbound string      `json:"inbound"`
	DNS     *DNSTrace   `json:"dns,omitempty"`
	Steps   []TraceStep `json:"steps"`

	// Row и Action — сработавшая строка маршрутизатора и ее действие.
	Row    string `json:"row"`
	Action string `json:"action"`

	// Chain — outbound-ы от цели правила до конечного, Outbound — конечный outbound.
	Chain    []string `json:"chain"`
	Outbound string   `json:"outbound,omitempty"`

	// Nodes и Edges — узлы и связи карты на пути соединения, DNSNodes и DNSEdges — на пути DNS-запроса.
	Nodes    []string `json:"nodes"`
	Edges    []string `json:"edges"`
	DNSNodes []string `json:"dns_nodes"`
	DNSEdges []string `json:"dns_edges"`
	Notes    []string `json:"notes"`
}

// tracer хранит состояние одной трассировки.
type tracer struct {
	env    TraceEnv
	result *TraceResult

	domain  string
	ips     []netip.Addr
	inbound string
}

// Trace определяет, по какому пути рабочего конфига config пойдет соединение к домену или IP.
func Trace(ctx context.Context, config map[string]any, request TraceRequest, env TraceEnv) (*TraceResult, error) {
	domain, addr, err := parseQuery(request.Query)
	if err != nil {
		return nil, err
	}

	inbound, err := pickInbound(config, request.Inbound)
	if err != nil {
		return nil, err
	}

	t := &tracer{
		env: env,
		result: &TraceResult{
			Query:        strings.TrimSpace(request.Query),
			Domain:       domain,
			IPs:          []string{},
			Resolved:     "",
			ResolveError: "",
			Inbound:      inbound,
			DNS:          nil,
			Steps:        []TraceStep{},
			Row:          "",
			Action:       "",
			Chain:        []string{},
			Outbound:     "",
			Nodes:        []string{InboundID(inbound), RouterID},
			Edges:        []string{InboundEdgeID(inbound)},
			DNSNodes:     []string{},
			DNSEdges:     []string{},
			Notes:        []string{},
		},
		domain:  domain,
		ips:     nil,
		inbound: inbound,
	}

	if addr.IsValid() {
		t.ips = []netip.Addr{addr}
		t.note("Для IP-адреса домен неизвестен: доменные правила не проверяются. sing-box узнает домен из sniff или DNS-кэша")
	} else {
		t.traceDNS(config)
		t.resolve(ctx, config)
	}

	for _, ip := range t.ips {
		t.result.IPs = append(t.result.IPs, ip.String())
	}

	t.traceRoute(config)

	return t.result, nil
}

// note добавляет примечание к результату.
func (t *tracer) note(format string, args ...any) {
	t.result.Notes = append(t.result.Notes, fmt.Sprintf(format, args...))
}

// highlight добавляет узел и связь на путь соединения.
func (t *tracer) highlight(node, edge string) {
	appendUnique(&t.result.Nodes, node)
	appendUnique(&t.result.Edges, edge)
}

// highlightDNS добавляет узел и связь на путь DNS-запроса.
func (t *tracer) highlightDNS(node, edge string) {
	appendUnique(&t.result.DNSNodes, node)
	appendUnique(&t.result.DNSEdges, edge)
}

// appendUnique добавляет в список непустое значение, которого в нем еще нет.
func appendUnique(list *[]string, value string) {
	if value != "" && !slices.Contains(*list, value) {
		*list = append(*list, value)
	}
}

// resolve находит адреса домена: сначала в hosts-серверах конфига (DNS-записи конфигуратора),
// затем системным резолвером.
func (t *tracer) resolve(ctx context.Context, config map[string]any) {
	dns, _ := config["dns"].(map[string]any)

	for _, server := range objects(dns["servers"]) {
		if jsonmap.String(server, "type") != "hosts" {
			continue
		}

		predefined, _ := server["predefined"].(map[string]any)

		for _, value := range jsonmap.Strings(predefined, t.domain) {
			if addr, err := netip.ParseAddr(value); err == nil {
				t.ips = append(t.ips, addr)
			}
		}

		if len(t.ips) > 0 {
			t.result.Resolved = ResolvedHosts

			return
		}
	}

	if t.env.Resolver == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	addrs, err := t.env.Resolver(ctx, t.domain)
	if err != nil {
		t.result.ResolveError = err.Error()
		t.note("Домен не резолвится, IP-правила не проверены")

		return
	}

	for _, addr := range addrs {
		t.ips = append(t.ips, addr.Unmap())
	}

	t.result.Resolved = ResolvedSystem
}

// traceDNS находит DNS-правило, которое обработает A-запрос домена.
func (t *tracer) traceDNS(config map[string]any) {
	dns, _ := config["dns"].(map[string]any)

	for _, rule := range parseDNSRules(dns, nil) {
		if rule.raw == nil {
			t.setDNS(config, rule, "ни одно DNS-правило не подошло", nil)

			return
		}

		matched, reason, matches, _ := t.evalRule(rule.raw, true)
		if !matched {
			continue
		}

		// route-options меняет параметры запроса и не завершает обработку.
		if rule.action == "route-options" {
			continue
		}

		t.setDNS(config, rule, reason, matches)

		return
	}
}

// setDNS запоминает сработавшее DNS-правило и outbound-ы, через которые запрос уйдет к серверу,
// и подсвечивает их на карте.
func (t *tracer) setDNS(config map[string]any, rule parsedRule, reason string, matches []rules.Match) {
	server := strings.TrimPrefix(rule.row.Target, "dns:")

	t.result.DNS = &DNSTrace{
		Row:      rule.row.ID,
		Label:    rule.row.Label,
		Action:   rule.action,
		Server:   server,
		Reason:   reason,
		Matches:  matches,
		Chain:    []string{},
		Implicit: false,
	}

	t.highlightDNS(DNSRouterID, "")

	if rule.row.Target == "" {
		return
	}

	t.highlightDNS(rule.row.Target, DNSEdgeID(rule.row.ID))

	dns, _ := config["dns"].(map[string]any)

	for _, item := range objects(dns["servers"]) {
		if jsonmap.String(item, "tag") != server {
			continue
		}

		detour, implicit := DNSEgress(item)
		if detour == "" {
			return
		}

		t.result.DNS.Chain = t.resolveChain(config, detour)
		t.result.DNS.Implicit = implicit
		t.highlightDNS(OutboundID(detour), DetourEdgeID(server))

		for i := 1; i < len(t.result.DNS.Chain); i++ {
			t.highlightDNS(OutboundID(t.result.DNS.Chain[i]), MemberEdgeID(t.result.DNS.Chain[i-1], t.result.DNS.Chain[i]))
		}

		return
	}
}

// traceRoute проходит правила маршрутизации по порядку до первого завершающего.
func (t *tracer) traceRoute(config map[string]any) {
	for _, rule := range parseRouteRules(config, nil) {
		if rule.raw == nil {
			t.finish(config, rule, TraceStep{
				Row:     rule.row.ID,
				Label:   rule.row.Label,
				Action:  rule.action,
				Matched: true,
				Reason:  "ни одно правило не подошло",
				Matches: nil,
			})

			return
		}

		matched, reason, matches, unknown := t.evalRule(rule.raw, false)

		if unknown != "" {
			t.note("Правило «%s» содержит условие %s, которое не проверяется: реальный путь может отличаться", rule.row.Label, unknown)
		}

		step := TraceStep{
			Row:     rule.row.ID,
			Label:   rule.row.Label,
			Action:  rule.action,
			Matched: matched,
			Reason:  reason,
			Matches: matches,
		}

		// sniff, resolve и route-options не завершают маршрутизацию.
		if !matched || slices.Contains([]string{"sniff", "resolve", "route-options"}, rule.action) {
			t.result.Steps = append(t.result.Steps, step)

			continue
		}

		t.finish(config, rule, step)

		return
	}
}

// finish запоминает сработавшее правило и строит цепочку outbound-ов.
func (t *tracer) finish(config map[string]any, rule parsedRule, step TraceStep) {
	t.result.Steps = append(t.result.Steps, step)
	t.result.Row = rule.row.ID
	t.result.Action = rule.action

	if rule.row.Target == "" {
		return
	}

	t.highlight(rule.row.Target, RouteEdgeID(rule.row.ID))

	tag, isOutbound := strings.CutPrefix(rule.row.Target, "out:")
	if !isOutbound {
		return
	}

	t.result.Chain = t.resolveChain(config, tag)
	t.result.Outbound = t.result.Chain[len(t.result.Chain)-1]

	for i := 1; i < len(t.result.Chain); i++ {
		t.highlight(OutboundID(t.result.Chain[i]), MemberEdgeID(t.result.Chain[i-1], t.result.Chain[i]))
	}
}

// resolveChain проходит selector-ы и urltest-ы от outbound-а start до конечного outbound-а.
func (t *tracer) resolveChain(config map[string]any, start string) []string {
	items := map[string]map[string]any{}

	for _, item := range append(objects(config["outbounds"]), objects(config["endpoints"])...) {
		items[jsonmap.String(item, "tag")] = item
	}

	chain := []string{start}

	for len(chain) < maxChain {
		current := chain[len(chain)-1]
		item := items[current]
		itemType := jsonmap.String(item, "type")

		if itemType != "selector" && itemType != "urltest" {
			break
		}

		next := t.env.Proxies[current].Now

		if next == "" {
			next = jsonmap.String(item, "default")

			if members := jsonmap.Strings(item, "outbounds"); next == "" && len(members) > 0 {
				next = members[0]
			}

			t.note("Clash API не сообщил выбор %s: взят участник по конфигу", current)
		}

		if next == "" || slices.Contains(chain, next) {
			break
		}

		chain = append(chain, next)
	}

	return chain
}

// evalRule проверяет условия правила для трассируемого соединения (dnsQuery — для A-запроса домена).
// Возвращает совпадение, пояснение, совпавшие правила групп и условие, которое проверить нельзя.
func (t *tracer) evalRule(rule map[string]any, dnsQuery bool) (bool, string, []rules.Match, string) {
	if jsonmap.String(rule, "type") == "logical" {
		return false, "логические правила не проверяются", nil, "logical"
	}

	var (
		matches        []rules.Match
		reasons        []string
		hasAddress     bool
		addressMatched bool
	)

	for _, key := range conditionKeys(rule) {
		switch key {
		case "inbound":
			if !slices.Contains(jsonmap.Strings(rule, key), t.inbound) && !dnsQuery {
				return t.invert(rule, false, fmt.Sprintf("inbound %s не входит в правило", t.inbound), nil)
			}

		case "network":
			if !slices.Contains(jsonmap.Strings(rule, key), traceNetwork) {
				return t.invert(rule, false, "не TCP", nil)
			}

		case "port":
			if dnsQuery || !slices.Contains(ints(rule[key]), tracePort) {
				return t.invert(rule, false, fmt.Sprintf("порт не %d", tracePort), nil)
			}

		case "protocol":
			if dnsQuery || !slices.Contains(jsonmap.Strings(rule, key), traceProtocol) {
				return t.invert(rule, false, "другой протокол", nil)
			}

		case "query_type":
			if !dnsQuery || !slices.ContainsFunc(jsonmap.Strings(rule, key), func(value string) bool { return strings.EqualFold(value, "A") }) {
				return t.invert(rule, false, "другой тип запроса", nil)
			}

		case "rule_set":
			found, names := t.matchRuleSets(jsonmap.Strings(rule, key), dnsQuery)

			if len(found) == 0 {
				return t.invert(rule, false, "нет в наборах "+strings.Join(names, ", "), nil)
			}

			matches = append(matches, found...)
			reasons = append(reasons, "есть в наборе "+strings.Join(names, ", "))

		case "ip_is_private", "domain", "domain_suffix", "domain_keyword", "domain_regex", "ip_cidr":
			hasAddress = true

			if reason := t.matchAddress(rule, key, dnsQuery); reason != "" {
				addressMatched = true
				reasons = append(reasons, reason)
			}

		default:
			return false, fmt.Sprintf("условие %s не проверяется", key), nil, key
		}
	}

	if hasAddress && !addressMatched {
		return t.invert(rule, false, "адрес не подходит под условия", nil)
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "условия выполнены")
	}

	return t.invert(rule, true, strings.Join(reasons, "; "), matches)
}

// invert применяет invert правила к результату проверки.
func (t *tracer) invert(rule map[string]any, matched bool, reason string, matches []rules.Match) (bool, string, []rules.Match, string) {
	if !jsonmap.Bool(rule, "invert") {
		return matched, reason, matches, ""
	}

	return !matched, "инвертированное правило: " + reason, nil, ""
}

// matchRuleSets сопоставляет соединение с rule-set-ами tags. Для DNS-запроса IP-наборы не проверяются.
func (t *tracer) matchRuleSets(tags []string, dnsQuery bool) ([]rules.Match, []string) {
	names, _ := ruleSetNames(tags)
	matches := make([]rules.Match, 0)

	if t.env.Rules == nil {
		return matches, names
	}

	for _, tag := range tags {
		group, ip, ok := render.ParseRuleSetTag(tag)
		if !ok {
			continue
		}

		kind := rules.RuleSetKindDomain

		switch {
		case group == rules.BypassGroupName:
			kind = rules.RuleSetKindAll

		case ip:
			kind = rules.RuleSetKindIP
		}

		ips := t.ips

		if dnsQuery {
			ips = nil
		}

		matches = append(matches, t.env.Rules.MatchGroup(group, kind, t.domain, ips)...)
	}

	return matches, names
}

// matchAddress проверяет условие адреса назначения key. Возвращает пояснение при совпадении.
func (t *tracer) matchAddress(rule map[string]any, key string, dnsQuery bool) string {
	values := jsonmap.Strings(rule, key)

	switch key {
	case "ip_is_private":
		for _, ip := range t.ips {
			if !dnsQuery && !isPublic(ip) {
				return fmt.Sprintf("%s — приватный адрес", ip)
			}
		}

	case "domain":
		if t.domain != "" && slices.Contains(values, t.domain) {
			return "domain " + t.domain
		}

	case "domain_suffix":
		for _, value := range values {
			if t.domain != "" && matchSuffix(t.domain, value) {
				return "domain_suffix " + value
			}
		}

	case "domain_keyword":
		for _, value := range values {
			if t.domain != "" && strings.Contains(t.domain, value) {
				return "domain_keyword " + value
			}
		}

	case "domain_regex":
		for _, value := range values {
			if re, err := regexp.Compile(value); err == nil && t.domain != "" && re.MatchString(t.domain) {
				return "domain_regex " + value
			}
		}

	case "ip_cidr":
		for _, value := range values {
			if prefix, err := netip.ParsePrefix(value); err == nil && !dnsQuery && containsAny(prefix, t.ips) {
				return "ip_cidr " + value
			}
		}
	}

	return ""
}

// parseQuery разбирает запрос трассировки: домен, IP, адрес с портом или URL.
func parseQuery(query string) (string, netip.Addr, error) {
	value := strings.ToLower(strings.TrimSpace(query))

	if _, rest, ok := strings.Cut(value, "://"); ok {
		value = rest
	}

	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}

	if i := strings.LastIndex(value, "@"); i >= 0 {
		value = value[i+1:]
	}

	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}

	value = strings.TrimSuffix(strings.Trim(value, "[]"), ".")

	if addr, err := netip.ParseAddr(value); err == nil {
		return "", addr.Unmap(), nil
	}

	if value == "" || len(value) > 253 || !domainRe.MatchString(value) {
		return "", netip.Addr{}, ErrInvalidQuery
	}

	return value, netip.Addr{}, nil
}

// pickInbound возвращает inbound для трассировки: заданный, tun-in или первый из конфига.
func pickInbound(config map[string]any, requested string) (string, error) {
	tags := make([]string, 0)

	for _, inbound := range objects(config["inbounds"]) {
		if tag := jsonmap.String(inbound, "tag"); tag != "" {
			tags = append(tags, tag)
		}
	}

	switch {
	case requested != "":
		if !slices.Contains(tags, requested) {
			return "", fmt.Errorf("%w: %s", ErrUnknownInbound, requested)
		}

		return requested, nil

	case slices.Contains(tags, "tun-in"):
		return "tun-in", nil

	case len(tags) > 0:
		return tags[0], nil

	default:
		return "", ErrUnknownInbound
	}
}

// matchSuffix проверяет домен по суффиксу так же, как domain_suffix в sing-box.
func matchSuffix(domain, suffix string) bool {
	suffix = strings.ToLower(suffix)

	if strings.HasPrefix(suffix, ".") {
		return strings.HasSuffix(domain, suffix)
	}

	return domain == suffix || strings.HasSuffix(domain, "."+suffix)
}

// containsAny проверяет, что хотя бы один из адресов ips входит в подсеть prefix.
func containsAny(prefix netip.Prefix, ips []netip.Addr) bool {
	return slices.ContainsFunc(ips, prefix.Contains)
}

// isPublic повторяет проверку публичного адреса sing-box (ip_is_private — обратное).
func isPublic(ip netip.Addr) bool {
	return !(ip.IsPrivate() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified())
}
