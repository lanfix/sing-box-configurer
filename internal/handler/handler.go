package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/amnezia"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/happ"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/trafficmonitor"
	"github.com/lanfix/sing-box-configurer/internal/update"
)

type Handler struct {
	rulesManager             *rules.Manager
	dockerControllerProvider *dockercontroller.Provider
	singBoxConfigProvider    *singboxconfig.Provider
	outboundManager          *outbound.Manager
	clashAPI                 *singboxclashapi.ClashAPI
	trafficMonitor           *trafficmonitor.Monitor
	happManager              *happ.Manager
	updateService            *update.Service
	amneziaManager           *amnezia.Manager
	dnsRecordsManager        *dnsrecords.Manager
}

func NewHandler(
	rulesManager *rules.Manager,
	dockerControllerProvider *dockercontroller.Provider,
	singBoxConfigProvider *singboxconfig.Provider,
	outboundManager *outbound.Manager,
	clashAPI *singboxclashapi.ClashAPI,
	trafficMonitor *trafficmonitor.Monitor,
	happManager *happ.Manager,
	updateService *update.Service,
	amneziaManager *amnezia.Manager,
	dnsRecordsManager *dnsrecords.Manager,
) *Handler {
	return &Handler{
		rulesManager:             rulesManager,
		dockerControllerProvider: dockerControllerProvider,
		singBoxConfigProvider:    singBoxConfigProvider,
		outboundManager:          outboundManager,
		clashAPI:                 clashAPI,
		trafficMonitor:           trafficMonitor,
		happManager:              happManager,
		updateService:            updateService,
		amneziaManager:           amneziaManager,
		dnsRecordsManager:        dnsRecordsManager,
	}
}

func (h *Handler) GetRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	rulesList := h.rulesManager.GetRules()
	pendingCount := h.rulesManager.GetPendingCount()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rules":         rulesList,
		"pending_count": pendingCount,
	})
}

func (h *Handler) AddRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type        string `json:"type"`
		Value       string `json:"value"`
		Description string `json:"description"`
		Group       string `json:"group"`
		Bypass      bool   `json:"bypass"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Type == "" || req.Value == "" {
		http.Error(w, "Type and value are required", http.StatusBadRequest)
		return
	}

	if req.Type != "domain" && req.Type != "domain_suffix" && req.Type != "ip" && req.Type != "cidr" {
		http.Error(w, "Invalid type. Must be: domain, domain_suffix, ip, or cidr", http.StatusBadRequest)
		return
	}

	// Если группа не указана, используем default.
	if req.Group == "" {
		req.Group = "default"
	}

	rule := rules.Rule{
		ID:          uuid.New().String(),
		Type:        req.Type,
		Value:       req.Value,
		Description: req.Description,
		Group:       req.Group,
		Bypass:      req.Bypass,
	}

	if err := h.rulesManager.AddRule(rule); err != nil {
		log.Printf("Error adding rule: %v", err)
		http.Error(w, "Failed to add rule: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"rule":    rule,
	})
}

// AddRuleBulk добавляет несколько правил одновременно.
func (h *Handler) AddRuleBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type        string `json:"type"`
		Values      string `json:"values"`
		Description string `json:"description"`
		Group       string `json:"group"`
		Bypass      bool   `json:"bypass"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Type == "" || req.Values == "" {
		http.Error(w, "Type and values are required", http.StatusBadRequest)
		return
	}

	if req.Type != "domain" && req.Type != "domain_suffix" && req.Type != "ip" && req.Type != "cidr" {
		http.Error(w, "Invalid type. Must be: domain, domain_suffix, ip, or cidr", http.StatusBadRequest)
		return
	}

	if req.Group == "" {
		req.Group = "default"
	}

	result, err := h.rulesManager.AddRuleBulk(req.Type, req.Values, req.Description, req.Group, req.Bypass)
	if err != nil {
		log.Printf("Error in bulk add: %v", err)
		http.Error(w, "Failed to process bulk add: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := h.rulesManager.DeleteRule(req.ID); err != nil {
		log.Printf("Error deleting rule: %v", err)
		http.Error(w, "Failed to delete rule", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// EditRule редактирует правило.
func (h *Handler) EditRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Group       string `json:"group"`
		Bypass      bool   `json:"bypass"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	if req.Group == "" {
		http.Error(w, "Group is required", http.StatusBadRequest)
		return
	}

	if err := h.rulesManager.EditRule(req.ID, req.Description, req.Group, req.Bypass); err != nil {
		log.Printf("Error editing rule: %v", err)
		http.Error(w, "Failed to edit rule: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (h *Handler) ApplyRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.rulesManager.ApplyRules(); err != nil {
		log.Printf("Error applying rules: %v", err)
		http.Error(w, "Failed to apply rules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Rules applied successfully",
	})
}

func (h *Handler) GetRuleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Deprecated: используйте /api/ruleset/group вместо этого.
	ruleSet, err := h.rulesManager.GetRuleSet()

	writeRuleSet(w, r, ruleSet, err)
}

// GetRuleSetByGroup возвращает ruleset для конкретной группы.
func (h *Handler) GetRuleSetByGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	groupName := r.URL.Query().Get("group")
	if groupName == "" {
		http.Error(w, "Group parameter is required", http.StatusBadRequest)
		return
	}

	ruleSet, err := h.rulesManager.GetRuleSetByGroup(groupName, rules.RuleSetKindAll)

	writeRuleSet(w, r, ruleSet, err)
}

// GetRuleSetByGroupKind возвращает обработчик, отдающий ruleset группы только с доменами или только с IP/CIDR.
func (h *Handler) GetRuleSetByGroupKind(kind rules.RuleSetKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

			return
		}

		groupName := r.URL.Query().Get("group")
		if groupName == "" {
			http.Error(w, "Group parameter is required", http.StatusBadRequest)

			return
		}

		ruleSet, err := h.rulesManager.GetRuleSetByGroup(groupName, kind)

		writeRuleSet(w, r, ruleSet, err)
	}
}

// GetBypassRuleSet возвращает ruleset правил, исключенных из туннелирования sing-box.
func (h *Handler) GetBypassRuleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ruleSet, err := h.rulesManager.GetBypassRuleSet()

	writeRuleSet(w, r, ruleSet, err)
}

// writeRuleSet отправляет ruleset для sing-box. Пока набор не готов, отвечает 503: sing-box оставит
// прежний (закэшированный) набор, а при первом запуске без кэша не стартует до готовности набора.
//
// Ответ помечается ETag. Если набор не изменился, отвечает 304: sing-box не перезагружает набор, а для
// route_exclude_address_set не перезаписывает nftables-набор (иначе он пересоздается каждые update_interval).
func writeRuleSet(w http.ResponseWriter, r *http.Request, ruleSet rules.SingBoxRuleSet, err error) {
	if errors.Is(err, rules.ErrRuleSetNotReady) {
		log.Printf("Rule-set requested before it is ready: %v", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)

		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	body, err := json.Marshal(ruleSet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`

	w.Header().Set("ETag", etag)

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(body)
}

// GetGroups возвращает список всех групп.
func (h *Handler) GetGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	groups := h.rulesManager.GetGroups()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"groups": groups,
	})
}

// AddGroup добавляет новую группу.
func (h *Handler) AddGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		DefaultOutbound string `json:"default_outbound"`
		DNSServer       string `json:"dns_server"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	group := rules.Group{
		Name:            req.Name,
		Description:     req.Description,
		DefaultOutbound: req.DefaultOutbound,
		DNSServer:       req.DNSServer,
	}

	if err := h.rulesManager.AddGroup(group); err != nil {
		log.Printf("Error adding group: %v", err)
		http.Error(w, "Failed to add group: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"group":   group,
	})
}

// DeleteGroup удаляет группу.
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.rulesManager.DeleteGroup(req.Name); err != nil {
		log.Printf("Error deleting group: %v", err)
		http.Error(w, "Failed to delete group: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// EditGroup редактирует группу.
func (h *Handler) EditGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		DefaultOutbound string `json:"default_outbound"`
		DNSServer       string `json:"dns_server"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.rulesManager.EditGroup(req.Name, req.Description, req.DefaultOutbound, req.DNSServer); err != nil {
		log.Printf("Error editing group: %v", err)
		http.Error(w, "Failed to edit group: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// URL Sources endpoints

func (h *Handler) GetURLSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sources := h.rulesManager.GetURLSources()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"url_sources": sources,
	})
}

func (h *Handler) AddURLSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL         string `json:"url"`
		Description string `json:"description"`
		Interval    int    `json:"interval"`
		Group       string `json:"group"`
		Bypass      bool   `json:"bypass"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	if req.Interval <= 0 {
		req.Interval = 60 // default 60 minutes
	}

	// Если группа не указана, используем default.
	if req.Group == "" {
		req.Group = "default"
	}

	source := rules.URLSource{
		ID:          uuid.New().String(),
		URL:         req.URL,
		Description: req.Description,
		Interval:    req.Interval,
		Group:       req.Group,
		Bypass:      req.Bypass,
	}

	if err := h.rulesManager.AddURLSource(source); err != nil {
		log.Printf("Error adding URL source: %v", err)
		http.Error(w, "Failed to add URL source: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"source":  source,
	})
}

func (h *Handler) DeleteURLSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := h.rulesManager.DeleteURLSource(req.ID); err != nil {
		log.Printf("Error deleting URL source: %v", err)
		http.Error(w, "Failed to delete URL source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// RefreshURLSource сразу загружает правила URL-источника, не дожидаясь интервала обновления.
func (h *Handler) RefreshURLSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "ID is required")

		return
	}

	err := h.rulesManager.RefreshURLSource(req.ID)

	switch {
	case errors.Is(err, rules.ErrURLSourceNotFound):
		writeJSONError(w, http.StatusNotFound, "Источник не найден")

		return

	case errors.Is(err, rules.ErrURLSourceNotApplied):
		writeJSONError(w, http.StatusConflict, "Источник ещё не применён")

		return

	case err != nil:
		// Ошибка загрузки уже сохранена в статусе источника и видна в таблице.
		writeJSONError(w, http.StatusBadGateway, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// EditURLSource редактирует URL источник.
func (h *Handler) EditURLSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Group       string `json:"group"`
		Bypass      bool   `json:"bypass"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	if req.Group == "" {
		http.Error(w, "Group is required", http.StatusBadRequest)
		return
	}

	if err := h.rulesManager.EditURLSource(req.ID, req.Description, req.Group, req.Bypass); err != nil {
		log.Printf("Error editing URL source: %v", err)
		http.Error(w, "Failed to edit URL source: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (h *Handler) ApplyURLSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.rulesManager.ApplyURLSources(); err != nil {
		log.Printf("Error applying URL sources: %v", err)
		http.Error(w, "Failed to apply URL sources: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "URL sources applied successfully",
	})
}

func (h *Handler) ValidateURLSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	urlRuleSet, err := h.rulesManager.GatherRuleSetFromURL(req.URL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": err.Error(),
			"count": 0,
		})

		return
	}

	fmt.Printf("%v\n", urlRuleSet)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": true,
		"error": "",
		"count": urlRuleSet.Total(),
	})
}

func (h *Handler) GetURLSourceRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sourceID := r.URL.Query().Get("id")
	if sourceID == "" {
		http.Error(w, "ID parameter is required", http.StatusBadRequest)
		return
	}

	ruleSet, err := h.rulesManager.GetURLSourceRuleSet(sourceID)
	if err != nil {
		// Правила хранятся в памяти и появляются только после успешной загрузки источника.
		writeJSONError(w, http.StatusNotFound, "Правила источника ещё не загружены: последняя загрузка не удалась или ещё не выполнялась")

		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cidrList":       ruleSet.CidrList,
		"domains":        ruleSet.Domains,
		"domainSuffixes": ruleSet.DomainSuffixes,
	})
}

func (h *Handler) ReloadSingBox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Лейблы с контейнера sing-box, по которым сервис управления контейнерами найдет его и перезапустит.
	labels := map[string]string{
		"app":     "sing-box",
		"managed": "true",
	}

	if err := h.dockerControllerProvider.RestartContainersByLabels(labels); err != nil {
		log.Printf("Error restarting sing-box: %v", err)
		http.Error(w, "Failed to restart sing-box: "+err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Sing-box перезагружен успешно",
	})
}

// Outbounds endpoints

// GetOutbounds возвращает список всех VPN outbounds.
func (h *Handler) GetOutbounds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	outbounds, err := h.outboundManager.GetOutbounds()
	if err != nil {
		log.Printf("Error getting outbounds: %v", err)
		http.Error(w, "Failed to get outbounds: "+err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"outbounds": outbounds,
	})
}

// AddOutbound добавляет новый outbound из share-ссылки.
func (h *Handler) AddOutbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ShareURL string `json:"shareUrl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ShareURL == "" {
		http.Error(w, "Share URL is required", http.StatusBadRequest)
		return
	}

	outbound, err := h.outboundManager.AddOutboundFromShare(req.ShareURL)
	if err != nil {
		log.Printf("Error adding outbound: %v", err)
		http.Error(w, "Failed to add outbound: "+err.Error(), http.StatusBadRequest)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"outbound": outbound,
		"message":  "Outbound добавлен успешно",
	})
}

// DeleteOutbound удаляет outbound по тегу.
func (h *Handler) DeleteOutbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Tag string `json:"tag"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Tag == "" {
		http.Error(w, "Tag is required", http.StatusBadRequest)
		return
	}

	if err := h.outboundManager.DeleteOutbound(req.Tag); err != nil {
		log.Printf("Error deleting outbound: %v", err)
		http.Error(w, "Failed to delete outbound: "+err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Outbound удален успешно",
	})
}
