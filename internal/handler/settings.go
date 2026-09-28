package handler

import (
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// GetRestartStatus возвращает настройки плановой перезагрузки sing-box, следующий и последний запуск.
func (h *Handler) GetRestartStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.restartTask.Status())
}

// UpdateRestart сохраняет расписание плановой перезагрузки sing-box.
func (h *Handler) UpdateRestart(w http.ResponseWriter, r *http.Request) {
	var restart settings.Restart

	if !decodeJSON(w, r, &restart) {
		return
	}

	if err := h.settingsManager.UpdateRestart(restart); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Расписание перезагрузки сохранено")
}

// GetSettings возвращает общие настройки sing-box.
func (h *Handler) GetSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.settingsManager.Get())
}

// UpdateSettings сохраняет уровень логов и CORS-origin-ы Clash API.
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LogLevel     string   `json:"log_level"`
		AllowOrigins []string `json:"allow_origins"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.settingsManager.Update(req.LogLevel, req.AllowOrigins); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Настройки сохранены")
}

// RegenerateClashSecret создает новый секрет Clash API.
func (h *Handler) RegenerateClashSecret(w http.ResponseWriter, _ *http.Request) {
	if err := h.settingsManager.RegenerateSecret(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "Секрет Clash API обновлен, он вступит в силу после применения конфига")
}
