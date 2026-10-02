package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/lanfix/sing-box-configurer/internal/platform"
)

const (
	// defaultLogLines — сколько строк журнала отдавать по умолчанию.
	defaultLogLines = 500

	// maxLogLines — наибольшее число строк журнала за запрос.
	maxLogLines = 10000
)

// GetLogs возвращает последние строки журнала sing-box или конфигуратора (параметры source и lines).
func (h *Handler) GetLogs(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = platform.LogSourceSingBox
	}

	lines := defaultLogLines

	if raw := r.URL.Query().Get("lines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeJSONError(w, http.StatusBadRequest, "Число строк должно быть положительным числом")

			return
		}

		lines = min(parsed, maxLogLines)
	}

	text, err := h.logs.Read(r.Context(), source, lines)
	if errors.Is(err, platform.ErrUnknownLogSource) {
		writeJSONError(w, http.StatusBadRequest, "Неизвестный источник журнала: "+source)

		return
	}

	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Не удалось прочитать журнал: "+err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source":   source,
		"platform": h.platform,
		"text":     text,
	})
}
