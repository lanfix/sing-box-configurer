package handler

import (
	"errors"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/devices"
	"github.com/lanfix/sing-box-configurer/internal/render"
)

// deviceRequest — тело запросов добавления и изменения устройства.
type deviceRequest struct {
	MAC     string `json:"mac"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
}

// device возвращает устройство из запроса.
func (r deviceRequest) device() devices.Device {
	return devices.Device{
		MAC:     r.MAC,
		Name:    r.Name,
		Profile: r.Profile,
	}
}

// devicesResponse — данные страницы «Устройства».
type devicesResponse struct {
	devices.State

	// InConfig — rule-set-ы устройств есть в рабочем конфиге sing-box (иначе нужно применить конфиг).
	InConfig bool `json:"in_config"`
}

// GetDevices возвращает устройства, найденные в сети, и политику для неизвестных.
func (h *Handler) GetDevices(w http.ResponseWriter, r *http.Request) {
	h.devicesManager.RefreshSupport(r.Context())

	writeJSON(w, http.StatusOK, devicesResponse{
		State:    h.devicesManager.State(),
		InConfig: h.devicesInConfig(),
	})
}

// devicesInConfig проверяет, что рабочий конфиг sing-box берет rule-set-ы устройств у конфигуратора.
func (h *Handler) devicesInConfig() bool {
	config, err := h.singBox.ActualConfig()
	if err != nil {
		return false
	}

	route, _ := config["route"].(map[string]any)
	ruleSets, _ := route["rule_set"].([]any)

	for _, item := range ruleSets {
		ruleSet, _ := item.(map[string]any)

		if tag, _ := ruleSet["tag"].(string); tag == render.DeviceRuleSetTag(devices.ProfileBlocked) {
			return true
		}
	}

	return false
}

// GetDeviceAlerts возвращает число найденных устройств без профиля (точка в меню).
func (h *Handler) GetDeviceAlerts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"unknown": h.devicesManager.UnknownCount(),
	})
}

// ScanDevices перечитывает таблицу соседей хоста.
func (h *Handler) ScanDevices(w http.ResponseWriter, r *http.Request) {
	if err := h.devicesManager.Refresh(r.Context()); err != nil {
		writeJSONError(w, http.StatusBadGateway, "Не удалось прочитать сеть хоста: "+err.Error())

		return
	}

	writeJSON(w, http.StatusOK, devicesResponse{
		State:    h.devicesManager.State(),
		InConfig: h.devicesInConfig(),
	})
}

// AddDevice добавляет устройство с профилем.
func (h *Handler) AddDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.devicesManager.Add(req.device()); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Устройство добавлено")
}

// EditDevice меняет имя и профиль устройства.
func (h *Handler) EditDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.devicesManager.Edit(req.device())
	if errors.Is(err, devices.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "Устройство не найдено")

		return
	}

	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Устройство сохранено")
}

// DeleteDevice удаляет устройство: к нему снова применяется профиль по умолчанию.
func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.devicesManager.Delete(req.MAC)
	if errors.Is(err, devices.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "Устройство не найдено")

		return
	}

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "Устройство удалено")
}

// UpdateDeviceSettings сохраняет политику для неизвестных устройств.
func (h *Handler) UpdateDeviceSettings(w http.ResponseWriter, r *http.Request) {
	var settings devices.Settings

	if !decodeJSON(w, r, &settings) {
		return
	}

	if server := settings.DirectDNSServer; server != "" && !h.dnsServerExists(server) {
		writeJSONError(w, http.StatusBadRequest, "DNS-сервер "+server+" не найден")

		return
	}

	if err := h.devicesManager.UpdateSettings(settings); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Настройки устройств сохранены")
}

// GetDeviceRuleSet возвращает rule-set профиля устройств для sing-box.
func (h *Handler) GetDeviceRuleSet(w http.ResponseWriter, r *http.Request) {
	ruleSet, err := h.devicesManager.RuleSet(r.Context(), r.URL.Query().Get("profile"))
	if errors.Is(err, devices.ErrUnknownProfile) {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	writeRuleSet(w, r, ruleSet, err)
}

// dnsServerExists проверяет, что DNS-сервер с тегом tag объявлен.
func (h *Handler) dnsServerExists(tag string) bool {
	for _, server := range h.dnsManager.Get().Servers {
		if server.Tag == tag {
			return true
		}
	}

	return false
}
