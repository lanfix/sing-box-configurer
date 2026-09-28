package handler

import (
	"errors"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/inbounds"
)

// GetInbounds возвращает mixed-inbound-ы.
func (h *Handler) GetInbounds(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"mixed": h.inboundsManager.Mixed(),
	})
}

// AddMixedInbound добавляет mixed-inbound.
func (h *Handler) AddMixedInbound(w http.ResponseWriter, r *http.Request) {
	var mixed inbounds.Mixed

	if !decodeJSON(w, r, &mixed) {
		return
	}

	if err := h.inboundsManager.AddMixed(mixed); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Inbound добавлен")
}

// EditMixedInbound меняет параметры mixed-inbound-а (тег не меняется).
func (h *Handler) EditMixedInbound(w http.ResponseWriter, r *http.Request) {
	var mixed inbounds.Mixed

	if !decodeJSON(w, r, &mixed) {
		return
	}

	err := h.inboundsManager.EditMixed(mixed)

	switch {
	case errors.Is(err, inbounds.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "Inbound не найден")

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		writeSuccess(w, "Inbound обновлен")
	}
}

// DeleteMixedInbound удаляет mixed-inbound.
func (h *Handler) DeleteMixedInbound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag string `json:"tag"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.inboundsManager.DeleteMixed(req.Tag); err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())

		return
	}

	writeSuccess(w, "Inbound удален")
}
