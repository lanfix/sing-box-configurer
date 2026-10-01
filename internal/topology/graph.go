// Package topology строит карту трафика из рабочего конфига sing-box: inbound-ы, правила маршрутизации,
// selector-ы групп, urltest-ы, outbound-ы и DNS. Здесь же сопоставляются живые соединения Clash API
// со связями карты и трассируется путь домена или IP.
package topology

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/rules"
)

// Виды узлов карты.
const (
	KindInbound   = "inbound"
	KindRouter    = "router"
	KindDNSRouter = "dns-router"
	KindSelector  = "selector"
	KindURLTest   = "urltest"
	KindOutbound  = "outbound"
	KindAction    = "action"
	KindDNSServer = "dns-server"
)

// Виды связей карты.
const (
	EdgeInbound = "inbound"
	EdgeRoute   = "route"
	EdgeMember  = "member"
	EdgeDNS     = "dns"
	EdgeDetour  = "detour"
)

// Идентификаторы служебных узлов.
const (
	RouterID    = "router"
	DNSRouterID = "dns"
	RejectID    = "action:reject"
	BypassID    = "action:bypass"

	// FinalRow — строка маршрутизатора с outbound-ом по умолчанию (route.final).
	FinalRow = "final"

	// DNSFinalRow — строка DNS-маршрутизатора с сервером по умолчанию (dns.final).
	DNSFinalRow = "dfinal"

	// RejectLabel — подпись узла отклонения: действия reject или outbound-а типа block.
	RejectLabel = "Отклонить"
)

// Graph — карта трафика.
type Graph struct {
	Nodes    []Node   `json:"nodes"`
	Edges    []Edge   `json:"edges"`
	Warnings []string `json:"warnings"`
}

// Node — узел карты.
type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Tag    string `json:"tag"`
	Type   string `json:"type"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`

	// Rows — строки маршрутизаторов (правила по порядку).
	Rows []Row `json:"rows,omitempty"`

	// Members и Now — участники selector-а или urltest-а и активный из них.
	Members []string `json:"members,omitempty"`
	Now     string   `json:"now,omitempty"`

	// Delay — последняя задержка по данным Clash API, мс (0 — нет замера или таймаут).
	Delay int `json:"delay,omitempty"`

	// Cluster — подписка, из которой пришел outbound: по ней outbound-ы сворачиваются на карте.
	Cluster string `json:"cluster,omitempty"`

	// Group — группа правил selector-а.
	Group *GroupInfo `json:"group,omitempty"`

	// Detour — outbound, через который DNS-сервер отправляет запросы. DetourImplicit — detour не задан,
	// и sing-box соединяется с сервером напрямую, мимо правил маршрутизации.
	Detour         string `json:"detour,omitempty"`
	DetourImplicit bool   `json:"detour_implicit,omitempty"`
}

// Row — строка маршрутизатора: одно правило и узел, куда оно направляет трафик.
type Row struct {
	ID     string   `json:"id"`
	Index  int      `json:"index"`
	Label  string   `json:"label"`
	Detail string   `json:"detail,omitempty"`
	Action string   `json:"action"`
	Target string   `json:"target,omitempty"`
	Groups []string `json:"groups,omitempty"`
}

// Edge — связь между узлами карты. SourceHandle — строка маршрутизатора, из которой выходит связь.
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"source_handle,omitempty"`
	Target       string `json:"target"`
	Kind         string `json:"kind"`
	Active       bool   `json:"active"`

	// Implicit — связь не задана в конфиге явно, а следует из поведения sing-box (DNS-сервер без detour).
	Implicit bool `json:"implicit,omitempty"`
}

// GroupInfo — группа правил конфигуратора.
type GroupInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	DNSServer   string           `json:"dns_server,omitempty"`
	Stats       rules.GroupStats `json:"stats"`
}

// ProxyState — состояние прокси по данным Clash API.
type ProxyState struct {
	Now   string
	Delay int
}

// Env — данные вне конфига sing-box, которыми дополняется карта.
type Env struct {
	// Proxies — состояние прокси из Clash API по тегам. nil, если Clash API недоступен.
	Proxies map[string]ProxyState

	// Groups — группы правил по именам, включая системные.
	Groups map[string]GroupInfo

	// Clusters — подпись подписки по тегу outbound-а.
	Clusters map[string]string
}

// InboundID возвращает идентификатор узла inbound-а.
func InboundID(tag string) string {
	return "in:" + tag
}

// OutboundID возвращает идентификатор узла outbound-а (в том числе selector-а и urltest-а).
func OutboundID(tag string) string {
	return "out:" + tag
}

// DNSServerID возвращает идентификатор узла DNS-сервера.
func DNSServerID(tag string) string {
	return "dns:" + tag
}

// InboundEdgeID возвращает идентификатор связи inbound-а с маршрутизатором.
func InboundEdgeID(tag string) string {
	return "e:in:" + tag
}

// RouteEdgeID возвращает идентификатор связи строки маршрутизатора с ее узлом.
func RouteEdgeID(row string) string {
	return "e:router:" + row
}

// DNSEdgeID возвращает идентификатор связи строки DNS-маршрутизатора с DNS-сервером.
func DNSEdgeID(row string) string {
	return "e:dns:" + row
}

// DetourEdgeID возвращает идентификатор связи DNS-сервера с outbound-ом, через который он работает.
func DetourEdgeID(server string) string {
	return "e:detour:" + server
}

// MemberEdgeID возвращает идентификатор связи группы outbound-ов с участником.
func MemberEdgeID(group, member string) string {
	return "e:" + OutboundID(group) + ">" + OutboundID(member)
}

// builder хранит состояние построения карты.
type builder struct {
	graph *Graph
	env   Env
	nodes map[string]bool
}

// Build строит карту трафика из рабочего конфига sing-box config.
func Build(config map[string]any, env Env) *Graph {
	b := &builder{
		graph: &Graph{
			Nodes:    []Node{},
			Edges:    []Edge{},
			Warnings: []string{},
		},
		env:   env,
		nodes: map[string]bool{},
	}

	if env.Proxies == nil {
		b.warn("Clash API недоступен: активные узлы selector-ов и urltest-ов показаны по конфигу")
	}

	b.addInbounds(config)
	b.addRouter(config)
	b.addOutbounds(config)
	b.addDNS(config)
	b.addActions()
	b.dropDanglingEdges()

	return b.graph
}

// warn добавляет предупреждение к карте.
func (b *builder) warn(format string, args ...any) {
	b.graph.Warnings = append(b.graph.Warnings, fmt.Sprintf(format, args...))
}

// addNode добавляет узел.
func (b *builder) addNode(node Node) {
	b.nodes[node.ID] = true
	b.graph.Nodes = append(b.graph.Nodes, node)
}

// addEdge добавляет связь.
func (b *builder) addEdge(edge Edge) {
	b.graph.Edges = append(b.graph.Edges, edge)
}

// addInbounds добавляет inbound-ы и их связи с маршрутизатором.
func (b *builder) addInbounds(config map[string]any) {
	for _, inbound := range objects(config["inbounds"]) {
		tag := jsonmap.String(inbound, "tag")
		if tag == "" {
			continue
		}

		b.addNode(newNode(InboundID(tag), KindInbound, tag, jsonmap.String(inbound, "type"), tag, inboundDetail(inbound)))

		b.addEdge(Edge{
			ID:           InboundEdgeID(tag),
			Source:       InboundID(tag),
			SourceHandle: "",
			Target:       RouterID,
			Kind:         EdgeInbound,
			Active:       true,
			Implicit:     false,
		})
	}
}

// addRouter добавляет маршрутизатор со строками правил и связи строк с узлами.
func (b *builder) addRouter(config map[string]any) {
	routeRules := parseRouteRules(config, b.env.Groups)
	rows := make([]Row, 0, len(routeRules))

	for _, rule := range routeRules {
		rows = append(rows, rule.row)

		if rule.row.Target == "" {
			continue
		}

		b.addEdge(Edge{
			ID:           RouteEdgeID(rule.row.ID),
			Source:       RouterID,
			SourceHandle: rule.row.ID,
			Target:       rule.row.Target,
			Kind:         EdgeRoute,
			Active:       true,
			Implicit:     false,
		})
	}

	router := newNode(RouterID, KindRouter, "", "route", "Маршрутизация", "")
	router.Rows = rows

	b.addNode(router)
}

// addOutbounds добавляет outbound-ы и endpoint-ы, а для selector-ов и urltest-ов — связи с участниками.
func (b *builder) addOutbounds(config map[string]any) {
	items := append(objects(config["outbounds"]), objects(config["endpoints"])...)

	for _, item := range items {
		tag := jsonmap.String(item, "tag")
		if tag == "" {
			continue
		}

		itemType := jsonmap.String(item, "type")
		state := b.env.Proxies[tag]

		node := Node{
			ID:             OutboundID(tag),
			Kind:           KindOutbound,
			Tag:            tag,
			Type:           itemType,
			Label:          tag,
			Detail:         outboundDetail(item),
			Rows:           nil,
			Members:        nil,
			Now:            "",
			Delay:          state.Delay,
			Cluster:        b.env.Clusters[tag],
			Group:          nil,
			Detour:         "",
			DetourImplicit: false,
		}

		switch itemType {
		case "block":
			node.Label = RejectLabel

		case "selector", "urltest":
			node.Kind = KindSelector

			if itemType == "urltest" {
				node.Kind = KindURLTest
			}

			node.Members = jsonmap.Strings(item, "outbounds")
			node.Now = groupNow(item, state)
			node.Detail = fmt.Sprintf("%s · %d", itemType, len(node.Members))

			if groupName, ok := strings.CutPrefix(tag, outbound.SelectorTagPrefix); ok && itemType == "selector" {
				node.Label = groupName

				if group, found := b.env.Groups[groupName]; found {
					node.Group = &group
				}
			}

			for _, member := range node.Members {
				b.addEdge(Edge{
					ID:           MemberEdgeID(tag, member),
					Source:       node.ID,
					SourceHandle: "",
					Target:       OutboundID(member),
					Kind:         EdgeMember,
					Active:       member == node.Now,
					Implicit:     false,
				})
			}
		}

		b.addNode(node)
	}
}

// addDNS добавляет DNS-маршрутизатор, DNS-серверы и связи серверов с outbound-ами (detour).
func (b *builder) addDNS(config map[string]any) {
	dns, _ := config["dns"].(map[string]any)
	servers := objects(dns["servers"])

	for _, server := range servers {
		tag := jsonmap.String(server, "tag")
		if tag == "" {
			continue
		}

		node := newNode(DNSServerID(tag), KindDNSServer, tag, jsonmap.String(server, "type"), tag, dnsServerDetail(server))
		node.Detour, node.DetourImplicit = DNSEgress(server)

		b.addNode(node)

		// Неявный direct рисуется, только если такой outbound есть: иначе связь некуда провести.
		if node.Detour == "" || (node.DetourImplicit && !b.nodes[OutboundID(node.Detour)]) {
			continue
		}

		b.addEdge(Edge{
			ID:           DetourEdgeID(tag),
			Source:       DNSServerID(tag),
			SourceHandle: "",
			Target:       OutboundID(node.Detour),
			Kind:         EdgeDetour,
			Active:       true,
			Implicit:     node.DetourImplicit,
		})
	}

	dnsRules := parseDNSRules(dns, b.env.Groups)
	rows := make([]Row, 0, len(dnsRules))

	for _, rule := range dnsRules {
		rows = append(rows, rule.row)

		if rule.row.Target == "" {
			continue
		}

		b.addEdge(Edge{
			ID:           DNSEdgeID(rule.row.ID),
			Source:       DNSRouterID,
			SourceHandle: rule.row.ID,
			Target:       rule.row.Target,
			Kind:         EdgeDNS,
			Active:       true,
			Implicit:     false,
		})
	}

	router := newNode(DNSRouterID, KindDNSRouter, "", "dns", "DNS", "")
	router.Rows = rows

	b.addNode(router)
}

// addActions добавляет узлы действий reject и bypass, если на них ссылается хотя бы одна связь. Правила reject
// ведут в outbound типа block, если он есть, поэтому узел reject появляется только без него.
func (b *builder) addActions() {
	actions := []Node{
		newNode(RejectID, KindAction, "", "reject", RejectLabel, "reject"),
		newNode(BypassID, KindAction, "", "bypass", "Мимо sing-box", "bypass"),
	}

	for _, action := range actions {
		if slices.ContainsFunc(b.graph.Edges, func(edge Edge) bool { return edge.Target == action.ID }) {
			b.addNode(action)
		}
	}
}

// dropDanglingEdges убирает связи с несуществующими узлами и предупреждает о них.
func (b *builder) dropDanglingEdges() {
	edges := make([]Edge, 0, len(b.graph.Edges))

	for _, edge := range b.graph.Edges {
		if b.nodes[edge.Source] && b.nodes[edge.Target] {
			edges = append(edges, edge)

			continue
		}

		b.warn("связь %s → %s пропущена: узел не найден", edge.Source, edge.Target)
	}

	b.graph.Edges = edges
}

// newNode возвращает узел без строк, участников, задержки, подписки и группы.
func newNode(id, kind, tag, nodeType, label, detail string) Node {
	return Node{
		ID:             id,
		Kind:           kind,
		Tag:            tag,
		Type:           nodeType,
		Label:          label,
		Detail:         detail,
		Rows:           nil,
		Members:        nil,
		Now:            "",
		Delay:          0,
		Cluster:        "",
		Group:          nil,
		Detour:         "",
		DetourImplicit: false,
	}
}

// DNSEgress возвращает outbound, через который DNS-сервер server отправляет запросы. Без detour sing-box
// соединяется с сервером обычным дайлером (implicit): напрямую, мимо правил маршрутизации — это direct.
// hosts, fakeip и resolved соединений не создают, tailscale работает через свой endpoint.
func DNSEgress(server map[string]any) (tag string, implicit bool) {
	switch jsonmap.String(server, "type") {
	case "hosts", "fakeip", "resolved", "predefined":
		return "", false

	case "tailscale":
		return jsonmap.String(server, "endpoint"), false
	}

	if detour := jsonmap.String(server, "detour"); detour != "" {
		return detour, false
	}

	return outbound.DirectTag, true
}

// groupNow возвращает активного участника selector-а или urltest-а: по Clash API, а если он недоступен —
// selector по умолчанию.
func groupNow(item map[string]any, state ProxyState) string {
	if state.Now != "" {
		return state.Now
	}

	if value := jsonmap.String(item, "default"); value != "" {
		return value
	}

	return ""
}

// inboundDetail возвращает подпись inbound-а: тип и адрес.
func inboundDetail(inbound map[string]any) string {
	itemType := jsonmap.String(inbound, "type")

	if port := jsonmap.Int(inbound, "listen_port"); port > 0 {
		return fmt.Sprintf("%s · :%d", itemType, port)
	}

	if name := jsonmap.String(inbound, "interface_name"); name != "" {
		return itemType + " · " + name
	}

	return itemType
}

// outboundDetail возвращает подпись outbound-а: тип и адрес сервера.
func outboundDetail(item map[string]any) string {
	itemType := jsonmap.String(item, "type")
	server := jsonmap.String(item, "server")

	if server == "" {
		for _, peer := range objects(item["peers"]) {
			if server = jsonmap.String(peer, "address"); server != "" {
				break
			}
		}
	}

	if server == "" {
		return itemType
	}

	return itemType + " · " + server
}

// dnsServerDetail возвращает подпись DNS-сервера: тип и адрес.
func dnsServerDetail(server map[string]any) string {
	serverType := jsonmap.String(server, "type")

	if address := jsonmap.String(server, "server"); address != "" {
		return serverType + " · " + address
	}

	return serverType
}

// objects возвращает объекты из JSON-массива value, пропуская элементы других типов.
func objects(value any) []map[string]any {
	items, _ := value.([]any)
	result := make([]map[string]any, 0, len(items))

	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}

	return result
}
