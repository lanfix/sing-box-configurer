package handler

import (
	"log"
	"net/http"
)

// GetHappProfiles возвращает профили Happ и ID инсталляции.
func (h *Handler) GetHappProfiles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"installation_id": h.happManager.InstallationID(),
		"profiles":        h.happManager.List(),
	})
}

// AddHappProfile добавляет профиль по ссылке на подписку.
func (h *Handler) AddHappProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	profile, err := h.happManager.Add(r.Context(), req.Name, req.URL)
	if err != nil {
		log.Printf("Error adding happ profile: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"message": "Подписка добавлена",
	})
}

// RefreshHappProfile заново загружает подписку профиля. Если серверы изменились и включено применение
// обновлений, они сразу переносятся в работающий sing-box — итог добавляется к сообщению.
func (h *Handler) RefreshHappProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	profile, applied, err := h.happManager.Refresh(r.Context(), req.ID)
	if err != nil {
		log.Printf("Error refreshing happ profile: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	message := "Подписка обновлена"

	if applied != "" {
		message += ". " + applied
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"message": message,
	})
}

// DeleteHappProfile удаляет профиль.
func (h *Handler) DeleteHappProfile(w http.ResponseWriter, r *http.Request) {
	h.happProfileAction(w, r, func(id string) (any, error) {
		return nil, h.happManager.Delete(id)
	}, "Подписка удалена")
}

// happProfileAction выполняет действие над профилем, ID которого передан в теле POST-запроса.
func (h *Handler) happProfileAction(w http.ResponseWriter, r *http.Request, action func(id string) (any, error), message string) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	profile, err := action(req.ID)
	if err != nil {
		log.Printf("Error in happ profile action: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"profile": profile,
		"message": message,
	})
}
