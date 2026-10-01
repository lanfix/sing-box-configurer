package handler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// GetSecurity возвращает защиту панели и адрес, по которому она открыта сейчас.
func (h *Handler) GetSecurity(w http.ResponseWriter, r *http.Request) {
	security := h.settingsManager.Get().Security

	writeJSON(w, http.StatusOK, map[string]any{
		"check_host":      security.CheckHost,
		"allowed_hosts":   security.AllowedHosts,
		"extra_hosts":     h.guard.ExtraHosts(),
		"current_host":    settings.Hostname(r.Host),
		"current_allowed": h.guard.HostAllowed(security, r.Host),
	})
}

// UpdateSecurity сохраняет защиту панели. Настройки, с которыми текущий адрес панели перестал бы открываться,
// не сохраняются: иначе пользователь сразу потерял бы доступ.
func (h *Handler) UpdateSecurity(w http.ResponseWriter, r *http.Request) {
	var req settings.Security

	if !decodeJSON(w, r, &req) {
		return
	}

	normalized, err := req.Normalize()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	if !h.guard.HostAllowed(normalized, r.Host) {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Панель открыта по адресу %s: добавьте его в список, иначе после сохранения панель станет недоступна", settings.Hostname(r.Host)))

		return
	}

	if err = h.settingsManager.UpdateSecurity(normalized); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	log.Printf("Panel security updated: check host %t, allowed hosts %v", normalized.CheckHost, normalized.AllowedHosts)
	writeSuccess(w, "Настройки безопасности сохранены")
}

// UpdateHappSettings сохраняет применение обновлений подписок Happ.
func (h *Handler) UpdateHappSettings(w http.ResponseWriter, r *http.Request) {
	var req settings.Happ

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.settingsManager.UpdateHapp(req); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "Настройки подписок сохранены")
}
