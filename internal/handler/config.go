package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/singbox"
)

// GetConfig возвращает отрендеренный конфиг sing-box, рабочий конфиг и признак различий.
func (h *Handler) GetConfig(w http.ResponseWriter, _ *http.Request) {
	state, err := h.singBox.State()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, state)
}

// GetConfigStatus возвращает, отличается ли отрендеренный конфиг от рабочего (для бейджа в меню).
func (h *Handler) GetConfigStatus(w http.ResponseWriter, _ *http.Request) {
	changed, err := h.singBox.Changed()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"changed": changed,
	})
}

// ApplyConfig проверяет отрендеренный конфиг, заменяет им рабочий и перезапускает sing-box.
func (h *Handler) ApplyConfig(w http.ResponseWriter, r *http.Request) {
	result, err := h.singBox.Apply(r.Context())

	switch {
	case errors.Is(err, singbox.ErrNoChanges):
		writeJSONError(w, http.StatusConflict, "Конфиг не изменился, применять нечего")

	case errors.Is(err, singbox.ErrCheckFailed):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())

	case err != nil:
		log.Printf("Cannot apply sing-box config: %v", err)
		writeJSONError(w, http.StatusBadGateway, err.Error())

	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"message":  result.Message,
			"warnings": result.Warnings,
			"backup":   result.Backup,
		})
	}
}

// ReloadSingBox перезапускает контейнер sing-box без изменения конфига.
func (h *Handler) ReloadSingBox(w http.ResponseWriter, _ *http.Request) {
	if err := h.singBox.Restart(); err != nil {
		log.Printf("Error restarting sing-box: %v", err)
		writeJSONError(w, http.StatusBadGateway, "Не удалось перезапустить sing-box: "+err.Error())

		return
	}

	writeSuccess(w, "sing-box перезапущен")
}
