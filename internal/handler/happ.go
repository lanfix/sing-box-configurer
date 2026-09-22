package handler

import (
	"encoding/json"
	"log"
	"net/http"
)

// GetHappProfiles возвращает профили Happ и ID инсталляции.
func (h *Handler) GetHappProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"installation_id": h.happManager.InstallationID(),
		"profiles":        h.happManager.List(),
	})
}

// AddHappProfile добавляет профиль по ссылке на подписку и добавляет его серверы во временный конфиг.
func (h *Handler) AddHappProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")

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
		"message": "Профиль добавлен, серверы записаны во временный конфиг",
	})
}

// RefreshHappProfile заново загружает подписку профиля.
func (h *Handler) RefreshHappProfile(w http.ResponseWriter, r *http.Request) {
	h.happProfileAction(w, r, func(id string) (any, error) {
		return h.happManager.Refresh(r.Context(), id)
	}, "Подписка обновлена")
}

// SyncHappProfile записывает серверы профиля во временный конфиг.
func (h *Handler) SyncHappProfile(w http.ResponseWriter, r *http.Request) {
	h.happProfileAction(w, r, func(id string) (any, error) {
		return h.happManager.Sync(id)
	}, "Серверы записаны во временный конфиг")
}

// DeleteHappProfile удаляет профиль и его серверы из временного конфига.
func (h *Handler) DeleteHappProfile(w http.ResponseWriter, r *http.Request) {
	h.happProfileAction(w, r, func(id string) (any, error) {
		return nil, h.happManager.Delete(id)
	}, "Профиль удалён, серверы убраны из временного конфига")
}

// happProfileAction выполняет действие над профилем, ID которого передан в теле POST-запроса.
func (h *Handler) happProfileAction(w http.ResponseWriter, r *http.Request, action func(id string) (any, error), message string) {
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

// writeJSON отправляет ответ в формате JSON.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}

// writeJSONError отправляет ошибку в формате JSON, который ожидает фронтенд ({"error": "..."}).
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}
