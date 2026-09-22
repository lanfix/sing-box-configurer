package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/amnezia"
)

// amneziaProfileView — профиль Amnezia с вычисляемыми полями для UI.
type amneziaProfileView struct {
	amnezia.Profile

	RequiresAWG bool `json:"requires_awg"`
	Synced      bool `json:"synced"`
	OutOfSync   bool `json:"out_of_sync"`
}

// GetAmneziaProfiles возвращает импортированные конфигурации Amnezia и поддержку AmneziaWG в sing-box.
func (h *Handler) GetAmneziaProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	profiles := h.amneziaManager.List()
	views := make([]amneziaProfileView, 0, len(profiles))

	for i := range profiles {
		views = append(views, amneziaProfileView{
			Profile:     profiles[i],
			RequiresAWG: profiles[i].RequiresAWG(),
			Synced:      len(profiles[i].SyncedTags) > 0,
			OutOfSync:   profiles[i].OutOfSync(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"awg_support": h.amneziaManager.AWGSupport(),
		"profiles":    views,
	})
}

// AddAmneziaProfile импортирует ключ vpn:// (свой сервер или подписку Amnezia Premium).
func (h *Handler) AddAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	profile, synced, err := h.amneziaManager.Add(r.Context(), req.Key, req.Name)
	if err != nil {
		log.Printf("Error importing amnezia key: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	message := "Конфигурация импортирована, серверы записаны во временный конфиг"

	if !synced {
		message = "Конфигурация сохранена, но не записана в конфиг: текущий sing-box не поддерживает AmneziaWG"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"synced":  synced,
		"profile": profile,
		"message": message,
	})
}

// SyncAmneziaProfile записывает серверы профиля во временный конфиг.
func (h *Handler) SyncAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	h.amneziaProfileAction(w, r, func(id string) (any, error) {
		return h.amneziaManager.Sync(id)
	}, "Серверы записаны во временный конфиг")
}

// RefreshAmneziaProfile заново запрашивает конфигурацию подписки Amnezia Premium у шлюза.
func (h *Handler) RefreshAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	h.amneziaProfileAction(w, r, func(id string) (any, error) {
		return h.amneziaManager.Refresh(r.Context(), id)
	}, "Конфигурация подписки обновлена")
}

// SetAmneziaCountry меняет страну сервера подписки Amnezia Premium.
func (h *Handler) SetAmneziaCountry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		ID      string `json:"id"`
		Country string `json:"country"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Country == "" {
		writeJSONError(w, http.StatusBadRequest, "Profile id and country are required")

		return
	}

	profile, err := h.amneziaManager.SetCountry(r.Context(), req.ID, req.Country)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"message": "Страна изменена, конфигурация обновлена",
	})
}

// DeleteAmneziaProfile удаляет профиль и его серверы из временного конфига.
func (h *Handler) DeleteAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "Profile id is required")

		return
	}

	warning, err := h.amneziaManager.Delete(r.Context(), req.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	message := "Конфигурация удалена, серверы убраны из временного конфига"

	if warning != "" {
		message += ". Внимание: " + warning
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"warning": warning,
		"message": message,
	})
}

// amneziaProfileAction выполняет действие над профилем, ID которого передан в теле POST-запроса.
func (h *Handler) amneziaProfileAction(w http.ResponseWriter, r *http.Request, action func(id string) (any, error), message string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "Profile id is required")

		return
	}

	profile, err := action(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"message": message,
	})
}
