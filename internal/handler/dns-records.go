package handler

import (
	"errors"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
)

// dnsRecordRequest — тело запросов добавления, редактирования и удаления DNS-записи.
type dnsRecordRequest struct {
	ID          string   `json:"id"`
	Domain      string   `json:"domain"`
	Addresses   []string `json:"addresses"`
	Description string   `json:"description"`
}

// GetDNSRecords возвращает список DNS-записей.
func (h *Handler) GetDNSRecords(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"records": h.dnsRecordsManager.List(),
	})
}

// AddDNSRecord добавляет DNS-запись.
func (h *Handler) AddDNSRecord(w http.ResponseWriter, r *http.Request) {
	var req dnsRecordRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	record, err := h.dnsRecordsManager.Add(req.Domain, req.Addresses, req.Description)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"record":  record,
	})
}

// EditDNSRecord редактирует DNS-запись.
func (h *Handler) EditDNSRecord(w http.ResponseWriter, r *http.Request) {
	var req dnsRecordRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.dnsRecordsManager.Edit(req.ID, req.Domain, req.Addresses, req.Description)

	switch {
	case errors.Is(err, dnsrecords.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		writeSuccess(w, "DNS-запись обновлена")
	}
}

// DeleteDNSRecord удаляет DNS-запись.
func (h *Handler) DeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	var req dnsRecordRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.dnsRecordsManager.Delete(req.ID)

	switch {
	case errors.Is(err, dnsrecords.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())

	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, err.Error())

	default:
		writeSuccess(w, "DNS-запись удалена")
	}
}
