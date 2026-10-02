package topology

import (
	"net"
	"slices"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
)

// Connection — активное соединение, привязанное к карте.
type Connection struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Destination string `json:"destination"`
	Network     string `json:"network"`
	Source      string `json:"source"`

	// Inbound — тег inbound-а, Row — строка маршрутизатора, по которой прошло соединение.
	Inbound string `json:"inbound"`
	Row     string `json:"row"`
	Rule    string `json:"rule"`

	// Chain — outbound-ы от цели правила до конечного прокси.
	Chain []string `json:"chain"`

	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    string `json:"start"`
}

// Live — соединения, привязанные к карте.
type Live struct {
	Connections []Connection `json:"connections"`
}

// MapConnections привязывает соединения Clash API к inbound-ам, строкам маршрутизатора и outbound-ам
// рабочего конфига config.
func MapConnections(config map[string]any, connections []singboxclashapi.Connection) *Live {
	routeRules := parseRouteRules(config, nil)
	result := make([]Connection, 0, len(connections))

	for _, conn := range connections {
		// Clash API отдает цепочку от конечного прокси к цели правила.
		chain := slices.Clone(conn.Chains)
		slices.Reverse(chain)

		var destination string

		if conn.Metadata.DestinationIP != "" {
			destination = net.JoinHostPort(conn.Metadata.DestinationIP, conn.Metadata.DestinationPort)
		}

		result = append(result, Connection{
			ID:          conn.ID,
			Host:        conn.Metadata.Host,
			Destination: destination,
			Network:     strings.ToLower(conn.Metadata.Network),
			Source:      conn.Metadata.SourceIP,
			Inbound:     InboundTag(conn.Metadata.Type),
			Row:         matchRow(routeRules, conn.Rule, chain),
			Rule:        conn.Rule,
			Chain:       chain,
			Upload:      conn.Upload,
			Download:    conn.Download,
			Start:       conn.Start,
		})
	}

	return &Live{
		Connections: result,
	}
}

// InboundTag возвращает тег inbound-а из поля type метаданных Clash API («тип/тег»).
func InboundTag(value string) string {
	if _, tag, ok := strings.Cut(value, "/"); ok {
		return tag
	}

	return value
}

// matchRow находит строку маршрутизатора, по которой прошло соединение. Сначала отбираются строки,
// ведущие в первый outbound цепочки, затем среди них — строка, все фрагменты которой есть в описании
// правила из Clash API.
func matchRow(routeRules []parsedRule, rule string, chain []string) string {
	if rule == "" || rule == FinalRow {
		return FinalRow
	}

	if len(chain) == 0 {
		return ""
	}

	conditions, _, _ := strings.Cut(rule, "=>")
	target := OutboundID(chain[0])
	fallback := ""

	for _, routeRule := range routeRules {
		if routeRule.raw == nil || routeRule.row.Target != target {
			continue
		}

		if fallback == "" {
			fallback = routeRule.row.ID
		}

		if len(routeRule.tokens) == 0 {
			continue
		}

		if !slices.ContainsFunc(routeRule.tokens, func(token string) bool { return !strings.Contains(conditions, token) }) {
			return routeRule.row.ID
		}
	}

	return fallback
}
