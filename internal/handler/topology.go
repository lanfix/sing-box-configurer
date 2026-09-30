package handler

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"

	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/topology"
)

// GetTopology возвращает карту трафика рабочего конфига sing-box.
func (h *Handler) GetTopology(w http.ResponseWriter, _ *http.Request) {
	config, err := h.singBox.ActualConfig()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Рабочий конфиг sing-box не прочитан: "+err.Error())

		return
	}

	clusters, err := h.singBox.ProxyClusters()
	if err != nil {
		log.Printf("Topology: cannot collect subscription outbounds: %v", err)
	}

	graph := topology.Build(config, topology.Env{
		Proxies:  h.proxyStates(),
		Groups:   h.groupInfos(),
		Clusters: clusters,
	})

	writeJSON(w, http.StatusOK, graph)
}

// GetTopologyConnections возвращает активные соединения, привязанные к карте трафика.
func (h *Handler) GetTopologyConnections(w http.ResponseWriter, _ *http.Request) {
	config, err := h.singBox.ActualConfig()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Рабочий конфиг sing-box не прочитан: "+err.Error())

		return
	}

	connections, err := h.clashAPI.GetConnections()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Clash API недоступен: "+err.Error())

		return
	}

	writeJSON(w, http.StatusOK, topology.MapConnections(config, connections.Connections))
}

// TraceTopology определяет путь соединения к домену или IP из параметра query через inbound.
func (h *Handler) TraceTopology(w http.ResponseWriter, r *http.Request) {
	config, err := h.singBox.ActualConfig()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Рабочий конфиг sing-box не прочитан: "+err.Error())

		return
	}

	result, err := topology.Trace(r.Context(), config, topology.TraceRequest{
		Query:   r.URL.Query().Get("query"),
		Inbound: r.URL.Query().Get("inbound"),
	}, topology.TraceEnv{
		Rules:    h.rulesManager,
		Proxies:  h.proxyStates(),
		Resolver: lookupHost,
	})

	if errors.Is(err, topology.ErrInvalidQuery) || errors.Is(err, topology.ErrUnknownInbound) {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// proxyStates возвращает активных участников групп и задержки из Clash API. nil — Clash API недоступен.
func (h *Handler) proxyStates() map[string]topology.ProxyState {
	proxies, err := h.clashAPI.GetProxies()
	if err != nil {
		log.Printf("Topology: cannot get clash proxies: %v", err)

		return nil
	}

	states := make(map[string]topology.ProxyState, len(proxies))

	for name, proxy := range proxies {
		state := topology.ProxyState{
			Now:   proxy.Now,
			Delay: 0,
		}

		if len(proxy.History) > 0 {
			state.Delay = proxy.History[len(proxy.History)-1].Delay
		}

		states[name] = state
	}

	return states
}

// groupInfos возвращает группы правил (включая системные) с размерами их наборов.
func (h *Handler) groupInfos() map[string]topology.GroupInfo {
	groups := h.rulesManager.GetAllGroups()
	infos := make(map[string]topology.GroupInfo, len(groups))

	for _, group := range groups {
		infos[group.Name] = topology.GroupInfo{
			Name:        group.Name,
			Description: group.Description,
			DNSServer:   group.DNSServer,
			Stats:       h.rulesManager.GroupStats(group.Name),
		}
	}

	return infos
}

// lookupHost резолвит домен системным резолвером.
func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// Проверка на этапе компиляции: менеджер правил подходит для трассировки.
var _ topology.RuleMatcher = (*rules.Manager)(nil)
