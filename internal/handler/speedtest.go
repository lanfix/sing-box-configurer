package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/settings"
	"github.com/lanfix/sing-box-configurer/internal/speedtest"
)

// GetSpeedTest возвращает серверы теста скорости, идущий замер и последние результаты.
func (h *Handler) GetSpeedTest(w http.ResponseWriter, _ *http.Request) {
	running, results := h.speedTest.Status()

	writeJSON(w, http.StatusOK, map[string]any{
		"settings": h.settingsManager.Get().SpeedTest,
		"running":  running,
		"results":  results,
	})
}

// RunSpeedTest замеряет скорость загрузки через outbound.
func (h *Handler) RunSpeedTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag    string `json:"tag"`
		Server string `json:"server"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	req.Tag = strings.TrimSpace(req.Tag)

	if req.Tag == "" {
		writeJSONError(w, http.StatusBadRequest, "Не указан outbound")

		return
	}

	if req.Tag == outbound.BlockTag {
		writeJSONError(w, http.StatusBadRequest, "Через block трафик не идет: замерять нечего")

		return
	}

	result, err := h.speedTest.Run(r.Context(), req.Tag, req.Server)

	switch {
	case errors.Is(err, speedtest.ErrBusy):
		writeJSONError(w, http.StatusConflict, "Уже идет другой замер, дождитесь его окончания")

	case errors.Is(err, speedtest.ErrUnknownServer):
		writeJSONError(w, http.StatusBadRequest, "Сервера нет в настройках теста скорости")

	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, err.Error())

	default:
		writeJSON(w, http.StatusOK, result)
	}
}

// UpdateSpeedTestSettings сохраняет серверы, длительность и число потоков теста скорости.
func (h *Handler) UpdateSpeedTestSettings(w http.ResponseWriter, r *http.Request) {
	var req settings.SpeedTest

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.settingsManager.UpdateSpeedTest(req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Настройки теста скорости сохранены")
}

// ResetSpeedTestSettings возвращает настройки теста скорости по умолчанию.
func (h *Handler) ResetSpeedTestSettings(w http.ResponseWriter, _ *http.Request) {
	if err := h.settingsManager.UpdateSpeedTest(settings.DefaultSpeedTest()); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "Настройки теста скорости сброшены")
}
