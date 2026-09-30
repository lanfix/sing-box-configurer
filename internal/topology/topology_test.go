package topology

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// fakeMatcher — правила групп: суффиксы доменов и подсети по имени группы.
type fakeMatcher struct {
	suffixes map[string][]string
	cidrs    map[string][]string
}

// MatchGroup сопоставляет домен и IP с правилами группы.
func (f fakeMatcher) MatchGroup(groupName string, kind rules.RuleSetKind, domain string, ips []netip.Addr) []rules.Match {
	matches := make([]rules.Match, 0)

	if kind != rules.RuleSetKindIP {
		for _, suffix := range f.suffixes[groupName] {
			if domain != "" && matchSuffix(domain, suffix) {
				matches = append(matches, rules.Match{
					Type:       "domain_suffix",
					Value:      suffix,
					Source:     rules.MatchSourceManual,
					SourceName: "",
				})
			}
		}
	}

	if kind != rules.RuleSetKindDomain {
		for _, cidr := range f.cidrs[groupName] {
			if containsAny(netip.MustParsePrefix(cidr), ips) {
				matches = append(matches, rules.Match{
					Type:       "ip_cidr",
					Value:      cidr,
					Source:     rules.MatchSourceManual,
					SourceName: "",
				})
			}
		}
	}

	return matches
}

// testConfig рендерит конфиг с группами default и work, urltest-ом auto, mixed-inbound-ом и DNS-записью,
// и разбирает его так же, как рабочий конфиг с диска.
func testConfig(t *testing.T) map[string]any {
	t.Helper()

	result, err := render.Render(render.Input{
		Groups: []rules.Group{
			{Name: "default", DefaultOutbound: "auto", DNSServer: "cloudflare"},
			{Name: "work", DefaultOutbound: "vless-2"},
		},
		DNS: dnsconfig.Data{
			Servers: []dnsconfig.Server{
				{Tag: "yandex", Type: dnsconfig.TypeUDP, Server: "77.88.8.1"},
				{Tag: "cloudflare", Type: dnsconfig.TypeHTTPS, Server: "1.1.1.1", Detour: "auto"},
			},
			Settings: dnsconfig.Settings{
				Final: "yandex",
			},
		},
		DNSRecords: []dnsrecords.Record{
			{Domain: "nas.home.lab", Addresses: []string{"192.168.50.8"}},
		},
		Outbounds: []map[string]any{
			{"type": "vless", "tag": "vless-1", "server": "a.example"},
			{"type": "vless", "tag": "vless-2", "server": "b.example"},
		},
		Subscriptions: nil,
		URLTests:      outbound.DefaultURLTests(),
		Mixed: []inbounds.Mixed{
			{Tag: "mixed-proxy", Listen: "0.0.0.0", ListenPort: 1080},
		},
		Settings:       settings.Settings{},
		RuleSetBaseURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(result.Config)
	if err != nil {
		t.Fatal(err)
	}

	var config map[string]any

	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}

	return config
}

// testProxies — состояние групп по Clash API: auto выбрал vless-1.
func testProxies() map[string]ProxyState {
	return map[string]ProxyState{
		"select-default": {Now: "auto", Delay: 0},
		"select-work":    {Now: "vless-2", Delay: 0},
		"auto":           {Now: "vless-1", Delay: 0},
		"vless-1":        {Now: "", Delay: 42},
	}
}

// testMatcher — google.com в группе default, 93.184.0.0/16 в группе work.
func testMatcher() fakeMatcher {
	return fakeMatcher{
		suffixes: map[string][]string{
			"default": {"google.com"},
		},
		cidrs: map[string][]string{
			"work": {"93.184.0.0/16"},
		},
	}
}

// findNode возвращает узел карты по идентификатору.
func findNode(graph *Graph, id string) *Node {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == id {
			return &graph.Nodes[i]
		}
	}

	return nil
}

// findRow возвращает строку маршрутизатора с группой groupName.
func findRow(t *testing.T, config map[string]any, groupName string) Row {
	t.Helper()

	for _, rule := range parseRouteRules(config, nil) {
		if slices.Contains(rule.row.Groups, groupName) && rule.action == "route" {
			return rule.row
		}
	}

	t.Fatalf("row of group %s not found", groupName)

	return Row{}
}

func TestBuild(t *testing.T) {
	config := testConfig(t)

	graph := Build(config, Env{
		Proxies: testProxies(),
		Groups: map[string]GroupInfo{
			"default": {Name: "default", Description: "Группа по умолчанию", DNSServer: "cloudflare", Stats: rules.GroupStats{}},
		},
		Clusters: map[string]string{"vless-2": "Happ · test"},
	})

	for _, id := range []string{
		InboundID("tun-in"), InboundID("mixed-proxy"), RouterID, DNSRouterID, RejectID,
		OutboundID("select-default"), OutboundID("auto"), OutboundID("direct"), DNSServerID("cloudflare"),
	} {
		if findNode(graph, id) == nil {
			t.Errorf("node %s not found", id)
		}
	}

	selector := findNode(graph, OutboundID("select-default"))

	if selector.Kind != KindSelector || selector.Label != "default" || selector.Now != "auto" || selector.Group == nil {
		t.Errorf("selector = %+v", selector)
	}

	if node := findNode(graph, OutboundID("vless-2")); node.Cluster != "Happ · test" {
		t.Errorf("cluster = %q", node.Cluster)
	}

	if node := findNode(graph, OutboundID("vless-1")); node.Delay != 42 {
		t.Errorf("delay = %d", node.Delay)
	}

	router := findNode(graph, RouterID)
	last := router.Rows[len(router.Rows)-1]

	if last.ID != FinalRow || last.Target != OutboundID("direct") {
		t.Errorf("final row = %+v", last)
	}

	active := map[string]bool{}

	for _, edge := range graph.Edges {
		active[edge.ID] = edge.Active
	}

	if !active[MemberEdgeID("select-default", "auto")] || active[MemberEdgeID("select-default", "vless-1")] {
		t.Errorf("selector member edges active = %v", active)
	}

	// Явный detour, неявный direct у сервера без detour и hosts без соединений.
	egress := map[string]Edge{}

	for _, edge := range graph.Edges {
		if edge.Kind == EdgeDetour {
			egress[edge.Source] = edge
		}
	}

	if edge := egress[DNSServerID("cloudflare")]; edge.Target != OutboundID("auto") || edge.Implicit {
		t.Errorf("cloudflare egress = %+v", edge)
	}

	if edge := egress[DNSServerID("yandex")]; edge.Target != OutboundID("direct") || !edge.Implicit {
		t.Errorf("yandex egress = %+v", edge)
	}

	if _, ok := egress[DNSServerID(render.HostsServerTag)]; ok {
		t.Error("hosts server must not have egress")
	}

	if node := findNode(graph, DNSServerID("yandex")); node.Detour != "direct" || !node.DetourImplicit {
		t.Errorf("yandex node = %+v", node)
	}

	if len(graph.Warnings) != 0 {
		t.Errorf("warnings = %v", graph.Warnings)
	}
}

func TestBuildWithoutClashAPI(t *testing.T) {
	graph := Build(testConfig(t), Env{
		Proxies:  nil,
		Groups:   nil,
		Clusters: nil,
	})

	// Без Clash API активным считается выбор по умолчанию из конфига.
	if node := findNode(graph, OutboundID("select-work")); node.Now != "vless-2" {
		t.Errorf("now = %q", node.Now)
	}

	if len(graph.Warnings) != 1 {
		t.Errorf("warnings = %v", graph.Warnings)
	}
}

func TestTrace(t *testing.T) {
	config := testConfig(t)

	env := TraceEnv{
		Rules:   testMatcher(),
		Proxies: testProxies(),
		Resolver: func(_ context.Context, host string) ([]netip.Addr, error) {
			if host == "example.org" {
				return []netip.Addr{netip.MustParseAddr("93.184.1.1")}, nil
			}

			return []netip.Addr{netip.MustParseAddr("142.250.1.1")}, nil
		},
	}

	tests := []struct {
		name     string
		query    string
		row      string
		chain    []string
		resolved string
		dns      string
	}{
		{
			name:     "domain of group default",
			query:    "https://www.google.com/search?q=1",
			row:      findRow(t, config, "default").ID,
			chain:    []string{"select-default", "auto", "vless-1"},
			resolved: ResolvedSystem,
			dns:      "cloudflare",
		},
		{
			name:     "domain resolved into ip of group work",
			query:    "example.org",
			row:      findRow(t, config, "work").ID,
			chain:    []string{"select-work", "vless-2"},
			resolved: ResolvedSystem,
			dns:      "yandex",
		},
		{
			name:     "dns record with private address",
			query:    "nas.home.lab",
			row:      "",
			chain:    []string{"direct"},
			resolved: ResolvedHosts,
			dns:      render.HostsServerTag,
		},
		{
			name:     "public ip without rules",
			query:    "8.8.8.8",
			row:      FinalRow,
			chain:    []string{"direct"},
			resolved: "",
			dns:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Trace(context.Background(), config, TraceRequest{Query: tt.query, Inbound: ""}, env)
			if err != nil {
				t.Fatal(err)
			}

			if tt.row != "" && result.Row != tt.row {
				t.Errorf("row = %s, want %s (steps %+v)", result.Row, tt.row, result.Steps)
			}

			if !slices.Equal(result.Chain, tt.chain) {
				t.Errorf("chain = %v, want %v", result.Chain, tt.chain)
			}

			if result.Resolved != tt.resolved {
				t.Errorf("resolved = %q, want %q", result.Resolved, tt.resolved)
			}

			if tt.dns != "" && (result.DNS == nil || result.DNS.Server != tt.dns) {
				t.Errorf("dns = %+v, want server %s", result.DNS, tt.dns)
			}

			if result.Inbound != "tun-in" || !slices.Contains(result.Edges, InboundEdgeID("tun-in")) {
				t.Errorf("inbound = %s, edges = %v", result.Inbound, result.Edges)
			}
		})
	}
}

func TestTraceHighlight(t *testing.T) {
	config := testConfig(t)

	result, err := Trace(context.Background(), config, TraceRequest{Query: "mail.google.com", Inbound: "mixed-proxy"}, TraceEnv{
		Rules:    testMatcher(),
		Proxies:  testProxies(),
		Resolver: nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	row := findRow(t, config, "default").ID

	for _, edge := range []string{InboundEdgeID("mixed-proxy"), RouteEdgeID(row), MemberEdgeID("select-default", "auto"), MemberEdgeID("auto", "vless-1")} {
		if !slices.Contains(result.Edges, edge) {
			t.Errorf("edge %s not highlighted: %v", edge, result.Edges)
		}
	}

	if result.Outbound != "vless-1" || len(result.Steps[len(result.Steps)-1].Matches) != 1 {
		t.Errorf("outbound = %s, steps = %+v", result.Outbound, result.Steps)
	}

	// DNS-запрос уходит к cloudflare через его detour auto; путь DNS подсвечивается отдельно.
	if result.DNS == nil || !slices.Equal(result.DNS.Chain, []string{"auto", "vless-1"}) || result.DNS.Implicit {
		t.Errorf("dns = %+v", result.DNS)
	}

	if !slices.Contains(result.DNSEdges, DetourEdgeID("cloudflare")) || slices.Contains(result.Edges, DetourEdgeID("cloudflare")) {
		t.Errorf("dns edges = %v, edges = %v", result.DNSEdges, result.Edges)
	}

	implicit, err := Trace(context.Background(), config, TraceRequest{Query: "example.com", Inbound: ""}, TraceEnv{
		Rules:    testMatcher(),
		Proxies:  testProxies(),
		Resolver: nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	if implicit.DNS.Server != "yandex" || !slices.Equal(implicit.DNS.Chain, []string{"direct"}) || !implicit.DNS.Implicit {
		t.Errorf("implicit dns = %+v", implicit.DNS)
	}
}

func TestTraceErrors(t *testing.T) {
	config := testConfig(t)

	if _, err := Trace(context.Background(), config, TraceRequest{Query: "не домен", Inbound: ""}, TraceEnv{}); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("err = %v, want ErrInvalidQuery", err)
	}

	if _, err := Trace(context.Background(), config, TraceRequest{Query: "google.com", Inbound: "missing"}, TraceEnv{}); !errors.Is(err, ErrUnknownInbound) {
		t.Errorf("err = %v, want ErrUnknownInbound", err)
	}
}

func TestParseQuery(t *testing.T) {
	tests := []struct {
		query  string
		domain string
		ip     string
	}{
		{query: "Google.COM.", domain: "google.com", ip: ""},
		{query: "https://user@www.youtube.com:8443/watch", domain: "www.youtube.com", ip: ""},
		{query: "1.2.3.4:443", domain: "", ip: "1.2.3.4"},
		{query: "[2001:db8::1]:443", domain: "", ip: "2001:db8::1"},
		{query: "2001:db8::1", domain: "", ip: "2001:db8::1"},
	}

	for _, tt := range tests {
		domain, addr, err := parseQuery(tt.query)
		if err != nil {
			t.Errorf("%s: %v", tt.query, err)

			continue
		}

		ip := ""
		if addr.IsValid() {
			ip = addr.String()
		}

		if domain != tt.domain || ip != tt.ip {
			t.Errorf("%s: domain = %q, ip = %q", tt.query, domain, ip)
		}
	}
}

func TestMapConnections(t *testing.T) {
	config := testConfig(t)
	row := findRow(t, config, "default").ID

	live := MapConnections(config, []singboxclashapi.Connection{
		{
			ID:     "1",
			Chains: []string{"vless-1", "auto", "select-default"},
			Rule:   "rule_set=[configurer-default configurer-default@ip] => route(select-default)",
			Metadata: singboxclashapi.ConnectionMetadata{
				Network:         "tcp",
				Type:            "tun/tun-in",
				Host:            "www.google.com",
				DestinationIP:   "142.250.1.1",
				DestinationPort: "443",
				SourceIP:        "192.168.1.10",
			},
		},
		{
			ID:     "2",
			Chains: []string{"direct"},
			Rule:   "final",
			Metadata: singboxclashapi.ConnectionMetadata{
				Network: "UDP",
				Type:    "mixed/mixed-proxy",
			},
		},
		{
			ID:     "3",
			Chains: []string{"direct"},
			Rule:   "ip_is_private=true => route(direct)",
			Metadata: singboxclashapi.ConnectionMetadata{
				Type: "tun/tun-in",
			},
		},
	})

	first := live.Connections[0]

	if first.Row != row || first.Inbound != "tun-in" || !slices.Equal(first.Chain, []string{"select-default", "auto", "vless-1"}) {
		t.Errorf("first = %+v", first)
	}

	if first.Destination != "142.250.1.1:443" {
		t.Errorf("destination = %s", first.Destination)
	}

	if second := live.Connections[1]; second.Row != FinalRow || second.Inbound != "mixed-proxy" || second.Network != "udp" {
		t.Errorf("second = %+v", second)
	}

	var privateRow string

	for _, rule := range parseRouteRules(config, nil) {
		if rule.row.Label == "Приватные IP" {
			privateRow = rule.row.ID
		}
	}

	if third := live.Connections[2]; third.Row != privateRow {
		t.Errorf("third row = %s, want %s", third.Row, privateRow)
	}
}
