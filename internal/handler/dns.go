package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
)

// GetDNS возвращает DNS-серверы, общие параметры и пользовательские правила.
func (h *Handler) GetDNS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.dnsManager.Get())
}

// AddDNSServer добавляет DNS-сервер.
func (h *Handler) AddDNSServer(w http.ResponseWriter, r *http.Request) {
	var server dnsconfig.Server

	if !decodeJSON(w, r, &server) {
		return
	}

	if err := h.dnsManager.AddServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "DNS-сервер добавлен")
}

// EditDNSServer меняет параметры DNS-сервера (тег не меняется).
func (h *Handler) EditDNSServer(w http.ResponseWriter, r *http.Request) {
	var server dnsconfig.Server

	if !decodeJSON(w, r, &server) {
		return
	}

	err := h.dnsManager.EditServer(server)

	switch {
	case errors.Is(err, dnsconfig.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "DNS-сервер не найден")

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		writeSuccess(w, "DNS-сервер обновлен")
	}
}

// DeleteDNSServer удаляет DNS-сервер, если он не используется группами и настройками.
func (h *Handler) DeleteDNSServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag string `json:"tag"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if groups := h.rulesManager.GroupsByDNSServer(req.Tag); len(groups) > 0 {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("DNS-сервер %s используется группами: %s", req.Tag, strings.Join(groups, ", ")))

		return
	}

	err := h.dnsManager.DeleteServer(req.Tag)

	switch {
	case errors.Is(err, dnsconfig.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "DNS-сервер не найден")

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		writeSuccess(w, "DNS-сервер удален")
	}
}

// UpdateDNSSettings сохраняет общие параметры DNS.
func (h *Handler) UpdateDNSSettings(w http.ResponseWriter, r *http.Request) {
	var settings dnsconfig.Settings

	if !decodeJSON(w, r, &settings) {
		return
	}

	if err := h.dnsManager.UpdateSettings(settings); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Настройки DNS сохранены")
}

// UpdateDNSRules заменяет пользовательские DNS-правила.
func (h *Handler) UpdateDNSRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []map[string]any `json:"rules"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Rules == nil {
		req.Rules = []map[string]any{}
	}

	if err := h.dnsManager.UpdateRules(req.Rules); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "DNS-правила сохранены")
}
