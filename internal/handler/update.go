package handler

import (
	"log"
	"net/http"
)

// CheckUpdates возвращает текущую и доступные версии. Параметр force=true сбрасывает кэш проверки.
func (h *Handler) CheckUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	writeJSON(w, http.StatusOK, h.updateService.Check(r.Context(), r.URL.Query().Get("force") == "true"))
}

// StartUpdate запускает обновление до указанной версии.
func (h *Handler) StartUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req struct {
		Version string `json:"version"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	status, err := h.updateService.Start(r.Context(), req.Version)
	if err != nil {
		log.Printf("Cannot start update: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, status)
}

// UpdateStatus возвращает состояние последнего обновления.
func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	status, err := h.updateService.Status(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, status)
}
