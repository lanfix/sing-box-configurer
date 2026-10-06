package handler

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/topology"
)

// maxCloseConnections ограничивает число соединений, закрываемых одним запросом.
const maxCloseConnections = 5000

// connectionView — активное соединение для страницы «Соединения».
type connectionView struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Destination string `json:"destination"`
	Network     string `json:"network"`
	Source      string `json:"source"`
	Inbound     string `json:"inbound"`
	Rule        string `json:"rule"`

	// SourceMAC и SourceName — устройство-источник из таблицы соседей хоста и его имя на странице «Устройства».
	SourceMAC  string `json:"source_mac,omitempty"`
	SourceName string `json:"source_name,omitempty"`

	// Group — группа правил, в selector которой ушло соединение. Пусто, если соединение ушло не в группу.
	Group string `json:"group,omitempty"`

	// Chain — outbound-ы от цели правила до конечного, Outbound — конечный outbound.
	Chain    []string `json:"chain"`
	Outbound string   `json:"outbound"`

	Process  string `json:"process,omitempty"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    string `json:"start"`
}

// GetConnections возвращает активные соединения sing-box с группой и цепочкой outbound-ов.
func (h *Handler) GetConnections(w http.ResponseWriter, _ *http.Request) {
	snapshot, err := h.clashAPI.GetConnections()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Clash API недоступен: "+err.Error())

		return
	}

	views := make([]connectionView, 0, len(snapshot.Connections))

	for _, conn := range snapshot.Connections {
		// Clash API отдает цепочку от конечного прокси к цели правила.
		chain := slices.Clone(conn.Chains)
		slices.Reverse(chain)

		view := connectionView{
			ID:          conn.ID,
			Host:        conn.Metadata.Host,
			Destination: "",
			Network:     strings.ToLower(conn.Metadata.Network),
			Source:      conn.Metadata.SourceIP,
			Inbound:     topology.InboundTag(conn.Metadata.Type),
			Rule:        conn.Rule,
			SourceMAC:   "",
			SourceName:  "",
			Group:       "",
			Chain:       chain,
			Outbound:    "",
			Process:     conn.Metadata.ProcessPath,
			Upload:      conn.Upload,
			Download:    conn.Download,
			Start:       conn.Start,
		}

		if conn.Metadata.DestinationIP != "" {
			view.Destination = net.JoinHostPort(conn.Metadata.DestinationIP, conn.Metadata.DestinationPort)
		}

		if conn.Metadata.SourceIP != "" && conn.Metadata.SourcePort != "" {
			view.Source = net.JoinHostPort(conn.Metadata.SourceIP, conn.Metadata.SourcePort)
		}

		if mac, name, ok := h.devicesManager.LookupIP(conn.Metadata.SourceIP); ok {
			view.SourceMAC = mac
			view.SourceName = name
		}

		if len(chain) > 0 {
			view.Outbound = chain[len(chain)-1]

			if group, ok := strings.CutPrefix(chain[0], outbound.SelectorTagPrefix); ok {
				view.Group = group
			}
		}

		views = append(views, view)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"connections":    views,
		"download_total": snapshot.DownloadTotal,
		"upload_total":   snapshot.UploadTotal,
		"memory":         snapshot.Memory,
	})
}

// CloseConnections закрывает соединения из списка ids или все соединения (all).
func (h *Handler) CloseConnections(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if req.All {
		if err := h.clashAPI.CloseAllConnections(); err != nil {
			writeJSONError(w, http.StatusBadGateway, "Не удалось закрыть соединения: "+err.Error())

			return
		}

		writeSuccess(w, "Все соединения закрыты")

		return
	}

	if len(req.IDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "Не выбрано ни одного соединения")

		return
	}

	if len(req.IDs) > maxCloseConnections {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("За раз можно закрыть не больше %d соединений", maxCloseConnections))

		return
	}

	closed := 0

	var lastErr error

	// Соединение могло закрыться само, пока его выбирали: ошибки отдельных соединений не прерывают остальные.
	for _, id := range req.IDs {
		if err := h.clashAPI.CloseConnection(id); err != nil {
			lastErr = err

			continue
		}

		closed++
	}

	if closed == 0 && lastErr != nil {
		writeJSONError(w, http.StatusBadGateway, "Не удалось закрыть соединения: "+lastErr.Error())

		return
	}

	writeSuccess(w, fmt.Sprintf("Закрыто соединений: %d", closed))
}
