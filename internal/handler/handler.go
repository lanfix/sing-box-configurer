package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/singbox"
)

type Handler struct {
	rulesManager        *rules.Manager
	dockerControllerAPI *dockercontroller.API
	configManager       *singbox.ConfigManager
}

func NewHandler(rulesManager *rules.Manager, dockerControllerAPI *dockercontroller.API, configManager *singbox.ConfigManager) *Handler {
	return &Handler{
		rulesManager:        rulesManager,
		dockerControllerAPI: dockerControllerAPI,
		configManager:       configManager,
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

	rule := rules.Rule{
		ID:          uuid.New().String(),
		Type:        req.Type,
		Value:       req.Value,
		Description: req.Description,
	}

	if err := h.rulesManager.AddRule(rule); err != nil {
		log.Printf("Error adding rule: %v", err)
		http.Error(w, "Failed to add rule", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"rule":    rule,
	})
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

	ruleSet := h.rulesManager.GetRuleSet()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ruleSet)
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

	source := rules.URLSource{
		ID:          uuid.New().String(),
		URL:         req.URL,
		Description: req.Description,
		Interval:    req.Interval,
	}

	if err := h.rulesManager.AddURLSource(source); err != nil {
		log.Printf("Error adding URL source: %v", err)
		http.Error(w, "Failed to add URL source", http.StatusInternalServerError)
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
		http.Error(w, "Error happened: "+err.Error(), http.StatusInternalServerError)
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

	if err := h.dockerControllerAPI.RestartContainersByLabels(labels); err != nil {
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

// Sing-Box Config Editor endpoints

func (h *Handler) GetSingBoxConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config, err := h.configManager.GetConfig()
	if err != nil {
		log.Printf("Error reading config: %v", err)
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)

		return
	}

	hasPending := h.configManager.HasPending()

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"config":     config,
		"hasPending": hasPending,
	})
}

func (h *Handler) SaveTempConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Config string `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Config == "" {
		http.Error(w, "Config is required", http.StatusBadRequest)
		return
	}

	if err := h.configManager.SaveTemp(req.Config); err != nil {
		log.Printf("Error saving temp config: %v", err)
		http.Error(w, "Failed to save temp config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Config saved to temporary storage",
	})
}

func (h *Handler) ApplySingBoxConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.configManager.HasPending() {
		http.Error(w, "No pending config to apply", http.StatusBadRequest)
		return
	}

	if err := h.configManager.ApplyConfig(); err != nil {
		log.Printf("Error applying config: %v", err)
		http.Error(w, "Failed to apply config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Restart sing-box container
	labels := map[string]string{
		"app":     "sing-box",
		"managed": "true",
	}

	if err := h.dockerControllerAPI.RestartContainersByLabels(labels); err != nil {
		log.Printf("Warning: config applied but failed to restart sing-box: %v", err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Config applied but failed to restart sing-box. Please restart manually.",
			"warning": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Config applied and sing-box restarted successfully",
	})
}

func (h *Handler) DiscardTempConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.configManager.DiscardTemp(); err != nil {
		log.Printf("Error discarding temp config: %v", err)
		http.Error(w, "Failed to discard temp config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Temporary config discarded",
	})
}
