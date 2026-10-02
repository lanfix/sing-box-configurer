package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
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

// GetConfigBackups возвращает резервные копии рабочего конфига sing-box.
func (h *Handler) GetConfigBackups(w http.ResponseWriter, _ *http.Request) {
	backups, err := h.singBox.Backups()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"backups": backups,
	})
}

// GetConfigBackup возвращает содержимое резервной копии (параметр name) в виде рендера.
func (h *Handler) GetConfigBackup(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")

	content, err := h.singBox.Backup(name)
	if errors.Is(err, singboxconfig.ErrBackupNotFound) {
		writeJSONError(w, http.StatusNotFound, "Резервная копия не найдена")

		return
	}

	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, "Резервная копия не читается: "+err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":    name,
		"content": string(content),
	})
}

// RestoreConfigBackup делает рабочим конфиг из резервной копии и перезапускает sing-box.
func (h *Handler) RestoreConfigBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	result, err := h.singBox.RestoreBackup(r.Context(), req.Name)

	switch {
	case errors.Is(err, singboxconfig.ErrBackupNotFound):
		writeJSONError(w, http.StatusNotFound, "Резервная копия не найдена")

	case errors.Is(err, singbox.ErrNoChanges):
		writeJSONError(w, http.StatusConflict, "Рабочий конфиг совпадает с резервной копией, восстанавливать нечего")

	case errors.Is(err, singbox.ErrCheckFailed):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())

	case err != nil:
		log.Printf("Cannot restore sing-box config backup %s: %v", req.Name, err)
		writeJSONError(w, http.StatusBadGateway, err.Error())

	default:
		log.Printf("sing-box config restored from backup %s (previous config: %s)", req.Name, result.Backup)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": result.Message,
			"backup":  result.Backup,
		})
	}
}

// ReloadSingBox перезапускает sing-box без изменения конфига.
func (h *Handler) ReloadSingBox(w http.ResponseWriter, r *http.Request) {
	if err := h.singBox.Restart(r.Context()); err != nil {
		log.Printf("Error restarting sing-box: %v", err)
		writeJSONError(w, http.StatusBadGateway, "Не удалось перезапустить sing-box: "+err.Error())

		return
	}

	writeSuccess(w, "sing-box перезапущен")
}
