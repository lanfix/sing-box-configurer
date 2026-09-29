package handler

import (
	"errors"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
)

// Источники outbound-ов в ответе API, кроме источников серверов из пакета outbound.
const (
	sourceBuiltin = "builtin"
	sourceURLTest = "urltest"
)

// outboundView — outbound для страницы Outbounds и выбора outbound-а группы.
type outboundView struct {
	ID         string         `json:"id,omitempty"`
	Tag        string         `json:"tag"`
	Type       string         `json:"type"`
	Server     string         `json:"server,omitempty"`
	Port       int            `json:"port,omitempty"`
	Source     string         `json:"source"`
	SourceName string         `json:"source_name,omitempty"`
	ProfileID  string         `json:"profile_id,omitempty"`
	Config     map[string]any `json:"config,omitempty"`
}

// newOutboundView собирает описание outbound-а из его объекта sing-box.
func newOutboundView(config map[string]any, source, sourceName, profileID string) outboundView {
	view := outboundView{
		ID:         "",
		Tag:        jsonmap.String(config, "tag"),
		Type:       jsonmap.String(config, "type"),
		Server:     jsonmap.String(config, "server"),
		Port:       jsonmap.Int(config, "server_port"),
		Source:     source,
		SourceName: sourceName,
		ProfileID:  profileID,
		Config:     nil,
	}

	// У WireGuard адрес сервера задается в первом peer-е.
	if peers, ok := config["peers"].([]any); ok && len(peers) > 0 {
		if peer, ok := peers[0].(map[string]any); ok {
			view.Server = jsonmap.String(peer, "address")
			view.Port = jsonmap.Int(peer, "port")
		}
	}

	return view
}

// GetOutbounds возвращает outbound-ы всех источников: urltest-ы, встроенные, добавленные вручную и из подписок.
func (h *Handler) GetOutbounds(w http.ResponseWriter, _ *http.Request) {
	views := make([]outboundView, 0)

	for _, urlTest := range h.outboundManager.ListURLTests() {
		views = append(views, outboundView{
			ID:         urlTest.ID,
			Tag:        urlTest.Tag,
			Type:       "urltest",
			Server:     "",
			Port:       0,
			Source:     sourceURLTest,
			SourceName: urlTest.Description,
			ProfileID:  "",
			Config:     nil,
		})
	}

	views = append(views,
		outboundView{Tag: outbound.DirectTag, Type: "direct", Source: sourceBuiltin, SourceName: "Прямое подключение"},
		outboundView{Tag: outbound.BlockTag, Type: "block", Source: sourceBuiltin, SourceName: "Блокировка"},
	)

	for _, item := range h.outboundManager.List() {
		view := newOutboundView(item.Config, outbound.SourceManual, "", "")
		view.ID = item.ID
		view.Config = item.Config

		views = append(views, view)
	}

	for _, profile := range h.happManager.List() {
		for _, server := range profile.Servers {
			views = append(views, newOutboundView(server.Outbound, outbound.SourceHapp, profile.Name, profile.ID))
		}
	}

	for _, profile := range h.amneziaManager.List() {
		for _, item := range profile.Items {
			views = append(views, newOutboundView(item.Config, outbound.SourceAmnezia, profile.Name, profile.ID))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outbounds": views,
	})
}

// AddOutbound добавляет outbound из share-ссылки.
func (h *Handler) AddOutbound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShareURL string `json:"shareUrl"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	item, err := h.outboundManager.AddFromShare(req.ShareURL)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Outbound "+item.Tag()+" добавлен")
}

// AddOutboundJSON добавляет outbound, заданный объектом sing-box.
func (h *Handler) AddOutboundJSON(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Config map[string]any `json:"config"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	item, err := h.outboundManager.Add(req.Config)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Outbound "+item.Tag()+" добавлен")
}

// EditOutbound заменяет объект outbound-а, добавленного вручную.
func (h *Handler) EditOutbound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string         `json:"id"`
		Config map[string]any `json:"config"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.outboundManager.Update(req.ID, req.Config)

	switch {
	case errors.Is(err, outbound.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "Outbound не найден")

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		writeSuccess(w, "Outbound обновлен")
	}
}

// DeleteOutbound удаляет outbound, добавленный вручную.
func (h *Handler) DeleteOutbound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.outboundManager.Delete(req.ID)

	switch {
	case errors.Is(err, outbound.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "Outbound не найден")

	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, err.Error())

	default:
		writeSuccess(w, "Outbound удален")
	}
}
