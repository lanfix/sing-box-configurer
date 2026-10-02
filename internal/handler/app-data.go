package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/appbackup"
)

// maxImportBody ограничивает размер файла импорта app.json.
const maxImportBody = 32 << 20

// importRequest — файл импорта: содержимое app.json текстом и параметры.
type importRequest struct {
	Content       string `json:"content"`
	ReplaceAccess bool   `json:"replace_access"`
}

// ExportAppData отдает app.json файлом для сохранения.
func (h *Handler) ExportAppData(w http.ResponseWriter, _ *http.Request) {
	data, err := h.appBackup.Export()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	filename := "sing-box-configurer-" + time.Now().Format("2006-01-02-150405") + ".json"

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	_, _ = w.Write(data)
}

// InspectAppData проверяет файл импорта и возвращает его содержимое без изменения данных.
func (h *Handler) InspectAppData(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeImport(w, r)
	if !ok {
		return
	}

	summary, err := h.appBackup.Inspect([]byte(req.Content))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   importErrorMessage(err),
			"summary": summary,
		})

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": summary,
	})
}

// ImportAppData заменяет данные приложения данными из файла и перезапускает конфигуратор.
func (h *Handler) ImportAppData(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeImport(w, r)
	if !ok {
		return
	}

	result, err := h.appBackup.Import([]byte(req.Content), appbackup.ImportOptions{
		ReplaceAccess: req.ReplaceAccess,
	})

	switch {
	case errors.Is(err, appbackup.ErrInvalidData), errors.Is(err, appbackup.ErrUnsupportedSchema):
		writeJSONError(w, http.StatusUnprocessableEntity, importErrorMessage(err))

		return

	case errors.Is(err, appbackup.ErrImported):
		writeJSONError(w, http.StatusConflict, "Данные уже загружены, конфигуратор перезапускается")

		return

	case err != nil:
		log.Printf("Cannot import app data: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "Не удалось загрузить данные: "+err.Error())

		return
	}

	log.Printf("App data imported (schema %d → %d, previous data: %s), restarting", result.FromVersion, result.ToVersion, result.Backup)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"message":    "Данные загружены, конфигуратор перезапускается",
		"result":     result,
		"restarting": true,
	})

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	h.restart()
}

// decodeImport разбирает запрос импорта. При ошибке отвечает 400 и возвращает false.
func decodeImport(w http.ResponseWriter, r *http.Request) (importRequest, bool) {
	var req importRequest

	if err := json.NewDecoder(io.LimitReader(r.Body, maxImportBody)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Некорректное тело запроса: "+err.Error())

		return req, false
	}

	if req.Content == "" {
		writeJSONError(w, http.StatusBadRequest, "Файл пустой")

		return req, false
	}

	return req, true
}

// importErrorMessage возвращает текст ошибки импорта для интерфейса без технического префикса.
func importErrorMessage(err error) string {
	message := err.Error()

	for _, prefix := range []error{appbackup.ErrInvalidData, appbackup.ErrUnsupportedSchema} {
		if trimmed, ok := strings.CutPrefix(message, prefix.Error()+": "); ok {
			return trimmed
		}
	}

	return message
}
