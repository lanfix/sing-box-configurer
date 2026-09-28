package handler

import (
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/amnezia"
)

// amneziaProfileView — профиль Amnezia с вычисляемыми полями для UI.
type amneziaProfileView struct {
	amnezia.Profile

	RequiresAWG bool `json:"requires_awg"`
}

// GetAmneziaProfiles возвращает импортированные конфигурации Amnezia и поддержку AmneziaWG в sing-box.
func (h *Handler) GetAmneziaProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles := h.amneziaManager.List()
	views := make([]amneziaProfileView, 0, len(profiles))

	for i := range profiles {
		views = append(views, amneziaProfileView{
			Profile:     profiles[i],
			RequiresAWG: profiles[i].RequiresAWG(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"awg_support": h.amneziaManager.AWGSupport(),
		"profiles":    views,
	})
}

// AddAmneziaProfile импортирует ключ vpn:// (свой сервер или подписку Amnezia Premium).
func (h *Handler) AddAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	profile, err := h.amneziaManager.Add(r.Context(), req.Key, req.Name)
	if err != nil {
		log.Printf("Error importing amnezia key: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	warning := ""

	if profile.RequiresAWG() && !h.amneziaManager.AWGSupport().Supported {
		warning = "текущий sing-box не поддерживает AmneziaWG: серверы не попадут в конфиг, пока не будет установлен sing-box-lx"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"warning": warning,
		"message": "Конфигурация импортирована",
	})
}

// RefreshAmneziaProfile заново запрашивает конфигурацию подписки Amnezia Premium у шлюза.
func (h *Handler) RefreshAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	profile, err := h.amneziaManager.Refresh(r.Context(), req.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"message": "Конфигурация подписки обновлена",
	})
}

// SetAmneziaCountry меняет страну сервера подписки Amnezia Premium.
func (h *Handler) SetAmneziaCountry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Country string `json:"country"`
	}

	if !decodeJSON(w, r, &req) {
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

// DeleteAmneziaProfile удаляет профиль. Для Amnezia Premium освобождается место устройства в подписке.
func (h *Handler) DeleteAmneziaProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	warning, err := h.amneziaManager.Delete(r.Context(), req.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	message := "Конфигурация удалена"

	if warning != "" {
		message += ". Внимание: " + warning
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"warning": warning,
		"message": message,
	})
}
