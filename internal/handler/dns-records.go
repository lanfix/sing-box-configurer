package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
)

// dnsRecordRequest — тело запросов добавления и редактирования DNS-записи.
type dnsRecordRequest struct {
	ID          string   `json:"id"`
	Domain      string   `json:"domain"`
	Addresses   []string `json:"addresses"`
	Description string   `json:"description"`
}

// GetDNSRecords возвращает список DNS-записей.
func (h *Handler) GetDNSRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": h.dnsRecordsManager.List(),
	})
}

// AddDNSRecord добавляет DNS-запись.
func (h *Handler) AddDNSRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req dnsRecordRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	record, err := h.dnsRecordsManager.Add(req.Domain, req.Addresses, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"record":  record,
	})
}

// EditDNSRecord редактирует DNS-запись.
func (h *Handler) EditDNSRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req dnsRecordRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	err := h.dnsRecordsManager.Edit(req.ID, req.Domain, req.Addresses, req.Description)
	if errors.Is(err, dnsrecords.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)

		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// DeleteDNSRecord удаляет DNS-запись.
func (h *Handler) DeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req dnsRecordRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	err := h.dnsRecordsManager.Delete(req.ID)
	if errors.Is(err, dnsrecords.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)

		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// SyncDNSRecords синхронизирует DNS-записи во временный конфиг sing-box.
func (h *Handler) SyncDNSRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if err := h.singBoxConfigProvider.SyncDNSRecordsToTemp(h.dnsRecordsManager.ConfigRecords()); err != nil {
		log.Printf("Error syncing dns records to config: %v", err)
		http.Error(w, "Failed to sync dns records: "+err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "DNS records synced to temporary config successfully",
	})
}

// CheckDNSRecordsSync проверяет, соответствуют ли DNS-записи актуальному или временному конфигу.
func (h *Handler) CheckDNSRecordsSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	targetPath := h.singBoxConfigProvider.GetActualPath()

	if h.singBoxConfigProvider.HasPending() {
		targetPath = h.singBoxConfigProvider.GetTempPath()
	}

	synced, err := h.singBoxConfigProvider.CheckDNSRecordsSyncAt(targetPath, h.dnsRecordsManager.ConfigRecords())
	if err != nil {
		log.Printf("Error checking dns records sync: %v", err)
		http.Error(w, "Failed to check dns records sync: "+err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"synced": synced,
	})
}
