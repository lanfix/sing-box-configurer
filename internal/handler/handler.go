// Package handler содержит HTTP API конфигуратора.
package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/amnezia"
	"github.com/lanfix/sing-box-configurer/internal/auth"
	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/happ"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/scheduler"
	"github.com/lanfix/sing-box-configurer/internal/settings"
	"github.com/lanfix/sing-box-configurer/internal/singbox"
	"github.com/lanfix/sing-box-configurer/internal/trafficmonitor"
	"github.com/lanfix/sing-box-configurer/internal/update"
)

// maxRequestBody ограничивает размер тела запроса.
const maxRequestBody = 8 << 20

// Deps — зависимости обработчиков.
type Deps struct {
	Auth           *auth.Manager
	Rules          *rules.Manager
	DNS            *dnsconfig.Manager
	DNSRecords     *dnsrecords.Manager
	Outbounds      *outbound.Manager
	Inbounds       *inbounds.Manager
	Settings       *settings.Manager
	RestartTask    *scheduler.RestartTask
	Happ           *happ.Manager
	Amnezia        *amnezia.Manager
	SingBox        *singbox.Service
	ClashAPI       *singboxclashapi.ClashAPI
	TrafficMonitor *trafficmonitor.Monitor
	Update         *update.Service
}

// Handler обрабатывает запросы API.
type Handler struct {
	auth              *auth.Manager
	rulesManager      *rules.Manager
	dnsManager        *dnsconfig.Manager
	dnsRecordsManager *dnsrecords.Manager
	outboundManager   *outbound.Manager
	inboundsManager   *inbounds.Manager
	settingsManager   *settings.Manager
	restartTask       *scheduler.RestartTask
	happManager       *happ.Manager
	amneziaManager    *amnezia.Manager
	singBox           *singbox.Service
	clashAPI          *singboxclashapi.ClashAPI
	trafficMonitor    *trafficmonitor.Monitor
	updateService     *update.Service
}

// NewHandler создает обработчики API.
func NewHandler(deps Deps) *Handler {
	return &Handler{
		auth:              deps.Auth,
		rulesManager:      deps.Rules,
		dnsManager:        deps.DNS,
		dnsRecordsManager: deps.DNSRecords,
		outboundManager:   deps.Outbounds,
		inboundsManager:   deps.Inbounds,
		settingsManager:   deps.Settings,
		restartTask:       deps.RestartTask,
		happManager:       deps.Happ,
		amneziaManager:    deps.Amnezia,
		singBox:           deps.SingBox,
		clashAPI:          deps.ClashAPI,
		trafficMonitor:    deps.TrafficMonitor,
		updateService:     deps.Update,
	}
}

// Register регистрирует маршруты API.
func (h *Handler) Register(mux *http.ServeMux) {
	// Вход в панель. status, login и logout доступны без входа (см. auth.Middleware).
	mux.HandleFunc("GET /api/auth/status", h.GetAuthStatus)
	mux.HandleFunc("POST /api/auth/login", h.Login)
	mux.HandleFunc("POST /api/auth/logout", h.Logout)
	mux.HandleFunc("GET /api/auth/settings", h.GetAuthSettings)
	mux.HandleFunc("POST /api/auth/settings", h.UpdateAuthSettings)

	// Rule-set-ы, которые забирает sing-box.
	mux.HandleFunc("GET /api/ruleset/group", h.GetRuleSetByGroupKind(rules.RuleSetKindAll))
	mux.HandleFunc("GET /api/ruleset/domain", h.GetRuleSetByGroupKind(rules.RuleSetKindDomain))
	mux.HandleFunc("GET /api/ruleset/ip", h.GetRuleSetByGroupKind(rules.RuleSetKindIP))
	mux.HandleFunc("GET /api/ruleset/bypass", h.GetBypassRuleSet)

	mux.HandleFunc("GET /api/rules", h.GetRules)
	mux.HandleFunc("POST /api/rules/add", h.AddRule)
	mux.HandleFunc("POST /api/rules/add-bulk", h.AddRuleBulk)
	mux.HandleFunc("POST /api/rules/edit", h.EditRule)
	mux.HandleFunc("POST /api/rules/delete", h.DeleteRule)
	mux.HandleFunc("POST /api/apply", h.ApplyRules)

	mux.HandleFunc("GET /api/groups", h.GetGroups)
	mux.HandleFunc("POST /api/groups/add", h.AddGroup)
	mux.HandleFunc("POST /api/groups/edit", h.EditGroup)
	mux.HandleFunc("POST /api/groups/delete", h.DeleteGroup)

	mux.HandleFunc("GET /api/url-sources", h.GetURLSources)
	mux.HandleFunc("POST /api/url-sources/add", h.AddURLSource)
	mux.HandleFunc("POST /api/url-sources/edit", h.EditURLSource)
	mux.HandleFunc("POST /api/url-sources/delete", h.DeleteURLSource)
	mux.HandleFunc("POST /api/url-sources/refresh", h.RefreshURLSource)
	mux.HandleFunc("POST /api/url-sources/apply", h.ApplyURLSources)
	mux.HandleFunc("POST /api/url-sources/validate", h.ValidateURLSource)
	mux.HandleFunc("GET /api/url-sources/rules", h.GetURLSourceRules)

	mux.HandleFunc("GET /api/dns", h.GetDNS)
	mux.HandleFunc("POST /api/dns/servers/add", h.AddDNSServer)
	mux.HandleFunc("POST /api/dns/servers/edit", h.EditDNSServer)
	mux.HandleFunc("POST /api/dns/servers/delete", h.DeleteDNSServer)
	mux.HandleFunc("POST /api/dns/settings", h.UpdateDNSSettings)
	mux.HandleFunc("POST /api/dns/advanced", h.UpdateDNSAdvanced)

	mux.HandleFunc("GET /api/dns-records", h.GetDNSRecords)
	mux.HandleFunc("POST /api/dns-records/add", h.AddDNSRecord)
	mux.HandleFunc("POST /api/dns-records/edit", h.EditDNSRecord)
	mux.HandleFunc("POST /api/dns-records/delete", h.DeleteDNSRecord)

	mux.HandleFunc("GET /api/outbounds", h.GetOutbounds)
	mux.HandleFunc("POST /api/outbounds/add", h.AddOutbound)
	mux.HandleFunc("POST /api/outbounds/add-json", h.AddOutboundJSON)
	mux.HandleFunc("POST /api/outbounds/edit", h.EditOutbound)
	mux.HandleFunc("POST /api/outbounds/delete", h.DeleteOutbound)

	mux.HandleFunc("GET /api/urltests", h.GetURLTests)
	mux.HandleFunc("POST /api/urltests/preview", h.PreviewURLTest)
	mux.HandleFunc("POST /api/urltests/add", h.AddURLTest)
	mux.HandleFunc("POST /api/urltests/edit", h.EditURLTest)
	mux.HandleFunc("POST /api/urltests/delete", h.DeleteURLTest)

	mux.HandleFunc("GET /api/inbounds", h.GetInbounds)
	mux.HandleFunc("POST /api/inbounds/mixed/add", h.AddMixedInbound)
	mux.HandleFunc("POST /api/inbounds/mixed/edit", h.EditMixedInbound)
	mux.HandleFunc("POST /api/inbounds/mixed/delete", h.DeleteMixedInbound)

	mux.HandleFunc("GET /api/settings", h.GetSettings)
	mux.HandleFunc("POST /api/settings", h.UpdateSettings)
	mux.HandleFunc("POST /api/settings/regenerate-secret", h.RegenerateClashSecret)
	mux.HandleFunc("GET /api/settings/restart", h.GetRestartStatus)
	mux.HandleFunc("POST /api/settings/restart", h.UpdateRestart)

	mux.HandleFunc("GET /api/config", h.GetConfig)
	mux.HandleFunc("GET /api/config/status", h.GetConfigStatus)
	mux.HandleFunc("POST /api/config/apply", h.ApplyConfig)
	mux.HandleFunc("POST /api/control/reload", h.ReloadSingBox)

	mux.HandleFunc("GET /api/clash/overview", h.GetClashOverview)
	mux.HandleFunc("GET /api/clash/proxies", h.GetClashProxies)
	mux.HandleFunc("POST /api/clash/proxies/select", h.SelectClashProxy)
	mux.HandleFunc("GET /api/clash/proxies/delay", h.TestClashProxyDelay)
	mux.HandleFunc("GET /api/clash/group/delay", h.TestClashGroupDelay)

	mux.HandleFunc("GET /api/topology", h.GetTopology)
	mux.HandleFunc("GET /api/topology/connections", h.GetTopologyConnections)
	mux.HandleFunc("GET /api/topology/trace", h.TraceTopology)

	mux.HandleFunc("GET /api/update/check", h.CheckUpdates)
	mux.HandleFunc("POST /api/update/start", h.StartUpdate)
	mux.HandleFunc("GET /api/update/status", h.UpdateStatus)

	mux.HandleFunc("GET /api/amnezia/profiles", h.GetAmneziaProfiles)
	mux.HandleFunc("POST /api/amnezia/profiles/add", h.AddAmneziaProfile)
	mux.HandleFunc("POST /api/amnezia/profiles/refresh", h.RefreshAmneziaProfile)
	mux.HandleFunc("POST /api/amnezia/profiles/country", h.SetAmneziaCountry)
	mux.HandleFunc("POST /api/amnezia/profiles/delete", h.DeleteAmneziaProfile)

	mux.HandleFunc("GET /api/subscriptions/alerts", h.GetSubscriptionAlerts)

	mux.HandleFunc("GET /api/happ/profiles", h.GetHappProfiles)
	mux.HandleFunc("POST /api/happ/profiles/add", h.AddHappProfile)
	mux.HandleFunc("POST /api/happ/profiles/refresh", h.RefreshHappProfile)
	mux.HandleFunc("POST /api/happ/profiles/delete", h.DeleteHappProfile)
}

// decodeJSON разбирает JSON-тело запроса в v. При ошибке отвечает 400 и возвращает false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Некорректное тело запроса: "+err.Error())

		return false
	}

	return true
}

// writeJSON отправляет ответ в формате JSON.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}

// writeJSONError отправляет ошибку в формате, который ожидает фронтенд ({"error": "..."}).
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

// writeSuccess отправляет ответ об успешном выполнении действия.
func writeSuccess(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": message,
	})
}
