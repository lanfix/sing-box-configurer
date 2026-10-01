package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
)

const (
	defaultDelayTestURL     = "http://www.gstatic.com/generate_204"
	defaultDelayTestTimeout = 5000
)

// GetClashProxies возвращает все прокси и прокси-группы из Clash API sing-box.
func (h *Handler) GetClashProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	proxies, err := h.clashAPI.GetProxies()
	if err != nil {
		log.Printf("Error getting clash proxies: %v", err)
		http.Error(w, "Failed to get proxies: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"proxies": proxies,
	})
}

// SelectClashProxy переключает активный элемент selector-группы.
func (h *Handler) SelectClashProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Group string `json:"group"`
		Name  string `json:"name"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Group == "" || req.Name == "" {
		http.Error(w, "Group and name are required", http.StatusBadRequest)
		return
	}

	if err := h.clashAPI.SelectProxy(req.Group, req.Name); err != nil {
		log.Printf("Error selecting clash proxy: %v", err)
		http.Error(w, "Failed to select proxy: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// TestClashProxyDelay замеряет задержку одного прокси.
func (h *Handler) TestClashProxyDelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name parameter is required", http.StatusBadRequest)
		return
	}

	testURL, timeout := delayTestParams(r)

	delay, err := h.clashAPI.TestProxyDelay(name, testURL, timeout)
	if err != nil {
		log.Printf("Error testing clash proxy delay: %v", err)
		http.Error(w, "Failed to test delay: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"delay": delay,
	})
}

// TestClashGroupDelay замеряет задержку всех элементов прокси-группы.
func (h *Handler) TestClashGroupDelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	group := r.URL.Query().Get("group")
	if group == "" {
		http.Error(w, "group parameter is required", http.StatusBadRequest)
		return
	}

	testURL, timeout := delayTestParams(r)

	delays, err := h.clashAPI.TestGroupDelay(group, testURL, timeout)
	if err != nil {
		log.Printf("Error testing clash group delay: %v", err)
		http.Error(w, "Failed to test group delay: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"delays": delays,
	})
}

// GetClashOverview собирает сводку для домашней вкладки: суммарный трафик,
// активные соединения, память, правила и окно измерений скорости.
func (h *Handler) GetClashOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var (
		wg          sync.WaitGroup
		connections *singboxclashapi.Connections
		connErr     error
		rulesCount  int
		version     string
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		connections, connErr = h.clashAPI.GetConnections()
	}()

	go func() {
		defer wg.Done()

		count, err := h.clashAPI.GetRulesCount()
		if err != nil {
			log.Printf("Error getting clash rules count: %v", err)
			return
		}

		rulesCount = count
	}()

	go func() {
		defer wg.Done()

		value, err := h.clashAPI.GetVersion()
		if err != nil {
			log.Printf("Error getting clash version: %v", err)
			return
		}

		version = value
	}()

	wg.Wait()

	if connErr != nil {
		log.Printf("Error getting clash connections: %v", connErr)
		http.Error(w, "Failed to get connections: "+connErr.Error(), http.StatusBadGateway)

		return
	}

	var tcp, udp int

	for _, conn := range connections.Connections {
		switch strings.ToLower(conn.Metadata.Network) {
		case "udp":
			udp++
		default:
			tcp++
		}
	}

	samples := h.trafficMonitor.Samples()
	if samples == nil {
		samples = []singboxclashapi.TrafficSample{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"downloadTotal": connections.DownloadTotal,
		"uploadTotal":   connections.UploadTotal,
		"memory":        connections.Memory,
		"connections": map[string]int{
			"total": len(connections.Connections),
			"tcp":   tcp,
			"udp":   udp,
		},
		"rules":           rulesCount,
		"version":         version,
		"traffic":         samples,
		"trafficStreamOk": h.trafficMonitor.Connected(),
	})
}

func delayTestParams(r *http.Request) (string, int) {
	testURL := r.URL.Query().Get("url")
	if testURL == "" {
		testURL = defaultDelayTestURL
	}

	timeout := defaultDelayTestTimeout

	if raw := r.URL.Query().Get("timeout"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	return testURL, timeout
}
