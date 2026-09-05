package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

func (h *Handler) GetSingBoxConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	configData, err := h.singBoxConfigProvider.GetConfig()
	if err != nil {
		log.Printf("Error reading config: %v", err)
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)

		return
	}

	hasPending := h.singBoxConfigProvider.HasPending()

	response := map[string]interface{}{
		"config":        string(configData),
		"appliedConfig": string(configData),
		"hasPending":    hasPending,
	}

	// Если есть временный конфиг, возвращаем его как текущий.
	if hasPending {
		tempConfigData, err := h.singBoxConfigProvider.GetTemp()
		if err != nil {
			log.Printf("Error reading temp config: %v", err)
			http.Error(w, "Failed to read temp config: "+err.Error(), http.StatusInternalServerError)

			return
		}

		response["config"] = string(tempConfigData)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
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

	tempConfigData := []byte(req.Config)

	configData, err := h.singBoxConfigProvider.GetConfig()
	if err != nil {
		log.Printf("Error reading temp config: %v", err)
		http.Error(w, "Failed to read temp config: "+err.Error(), http.StatusInternalServerError)

		return
	}

	// Если конфиги одинаковые, то можем смело удалять temp-конфиг.
	if slices.Equal(configData, tempConfigData) {
		_ = h.singBoxConfigProvider.RemoveTemp()

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"hasPending": false,
			"message":    "Config removed because it is the same as the main config.",
		})

		return
	}

	if err := h.singBoxConfigProvider.SaveTemp(tempConfigData); err != nil {
		log.Printf("Error saving temp config: %v", err)
		http.Error(w, "Failed to save temp config: "+err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"hasPending": true,
		"message":    "Config saved to temporary storage",
	})
}

func (h *Handler) ApplySingBoxConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.singBoxConfigProvider.HasPending() {
		http.Error(w, "No pending config to apply", http.StatusBadRequest)
		return
	}

	if err := h.singBoxConfigProvider.ApplyConfig(); err != nil {
		log.Printf("Error applying config: %v", err)
		http.Error(w, "Failed to apply config: "+err.Error(), http.StatusInternalServerError)

		return
	}

	// Синхронизируем группы в конфиг sing-box.
	groups := h.rulesManager.GetGroups()

	// Преобразуем группы в формат для singboxconfig.
	var configGroups []singboxconfig.Group

	for _, g := range groups {
		configGroups = append(configGroups, singboxconfig.Group{
			Name:        g.Name,
			Description: g.Description,
		})
	}

	actualConfigPath := h.singBoxConfigProvider.GetActualPath()
	if err := h.singBoxConfigProvider.SyncGroupsToConfig(actualConfigPath, configGroups); err != nil {
		log.Printf("Warning: failed to sync groups to config: %v", err)
	}

	// Перезапускаем контейнер sing-box.
	labels := map[string]string{
		"app":     "sing-box",
		"managed": "true",
	}

	if err := h.dockerControllerProvider.RestartContainersByLabels(labels); err != nil {
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

	if err := h.singBoxConfigProvider.RemoveTemp(); err != nil {
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

// CheckPendingConfig проверяет наличие несохраненных изменений конфига.
func (h *Handler) CheckPendingConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	hasPending := h.singBoxConfigProvider.HasPending()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"hasPending": hasPending,
	})
}
