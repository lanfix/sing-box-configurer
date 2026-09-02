package rules

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// GetURLSources returns all URL sources
func (rm *Manager) GetURLSources() []URLSource {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.data.URLSources
}

// AddURLSource adds a new URL source
func (rm *Manager) AddURLSource(source URLSource) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	source.Applied = false
	source.CreatedAt = time.Now()
	source.LastStatus = "pending"
	rm.data.URLSources = append(rm.data.URLSources, source)

	return rm.save()
}

// DeleteURLSource marks a URL source as deleted
func (rm *Manager) DeleteURLSource(id string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for i, source := range rm.data.URLSources {
		if source.ID == id {
			rm.data.URLSources[i].Deleted = true

			break
		}
	}

	return rm.save()
}

// ApplyURLSources applies all pending URL sources
func (rm *Manager) ApplyURLSources() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Remove deleted sources and mark active ones as applied
	newSources := make([]URLSource, 0)

	for i := range rm.data.URLSources {
		source := &rm.data.URLSources[i]

		if source.Deleted {
			rm.stopURLSourceUpdates(source.ID)

			rm.urlRulesMu.Lock()
			delete(rm.urlRules, source.ID)
			rm.urlRulesMu.Unlock()

			continue
		}

		if !source.Applied {
			source.Applied = true
			go rm.startURLSourceUpdates(*source)
		}

		newSources = append(newSources, *source)
	}

	rm.data.URLSources = newSources

	return rm.save()
}

// GetURLSourceRules returns the rules loaded from a specific URL source
func (rm *Manager) GetURLSourceRules(sourceID string) []string {
	rm.urlRulesMu.RLock()
	defer rm.urlRulesMu.RUnlock()

	if rules, exists := rm.urlRules[sourceID]; exists {
		return append([]string{}, rules...) // return a copy
	}

	return []string{}
}

// stopURLSourceUpdates stops the periodic update goroutine for a URL source
func (rm *Manager) stopURLSourceUpdates(sourceID string) {
	rm.cancelFuncsMu.Lock()
	defer rm.cancelFuncsMu.Unlock()

	if cancel, exists := rm.cancelFuncs[sourceID]; exists {
		cancel()
		delete(rm.cancelFuncs, sourceID)
	}
}

// StartAllURLSourceUpdates starts periodic updates for all applied URL sources
func (rm *Manager) StartAllURLSourceUpdates() {
	rm.mu.RLock()
	sources := make([]URLSource, len(rm.data.URLSources))
	copy(sources, rm.data.URLSources)
	rm.mu.RUnlock()

	for _, source := range sources {
		if source.Applied && !source.Deleted {
			go rm.startURLSourceUpdates(source)
		}
	}
}

// startURLSourceUpdates starts periodic updates for a URL source
func (rm *Manager) startURLSourceUpdates(source URLSource) {
	ctx, cancel := context.WithCancel(context.Background())

	rm.cancelFuncsMu.Lock()
	rm.cancelFuncs[source.ID] = cancel
	rm.cancelFuncsMu.Unlock()

	// Fetch immediately
	rm.fetchURLSource(source.ID)

	ticker := time.NewTicker(time.Duration(source.Interval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.fetchURLSource(source.ID)
		}
	}
}

// fetchURLSource fetches IP/CIDR list from URL
func (rm *Manager) fetchURLSource(sourceID string) {
	rm.mu.Lock()

	var source *URLSource

	for i := range rm.data.URLSources {
		if rm.data.URLSources[i].ID == sourceID {
			source = &rm.data.URLSources[i]

			break
		}
	}

	rm.mu.Unlock()

	if source == nil {
		return
	}

	log.Printf("Fetching URL source: %s (%s)", source.Description, source.URL)

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	resp, err := client.Get(source.URL)
	if err != nil {
		rm.updateURLSourceStatus(sourceID, "error", err.Error(), 0)
		log.Printf("Error fetching URL source %s: %v", source.Description, err)

		return
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		rm.updateURLSourceStatus(sourceID, "error", errMsg, 0)
		log.Printf("Error fetching URL source %s: %s", source.Description, errMsg)

		return
	}

	var ipCidrList []string

	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		// Validate IP or CIDR
		if isValidIPOrCIDR(line) {
			ipCidrList = append(ipCidrList, line)
		}
	}

	if err := scanner.Err(); err != nil {
		rm.updateURLSourceStatus(sourceID, "error", err.Error(), 0)
		log.Printf("Error reading URL source %s: %v", source.Description, err)
		return
	}

	// Update in-memory rules
	rm.urlRulesMu.Lock()
	rm.urlRules[sourceID] = ipCidrList
	rm.urlRulesMu.Unlock()

	// Update status
	rm.updateURLSourceStatus(sourceID, "success", "", len(ipCidrList))
	log.Printf("Successfully fetched %d items from URL source: %s", len(ipCidrList), source.Description)
}

// updateURLSourceStatus updates the status of a URL source
func (rm *Manager) updateURLSourceStatus(sourceID, status, errorMsg string, count int) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for i := range rm.data.URLSources {
		if rm.data.URLSources[i].ID == sourceID {
			rm.data.URLSources[i].LastUpdate = time.Now()
			rm.data.URLSources[i].LastStatus = status
			rm.data.URLSources[i].LastError = errorMsg
			rm.data.URLSources[i].ItemsCount = count
			break
		}
	}

	// Save updated status
	rm.save()
}

// isValidIPOrCIDR validates if a string is a valid IP address or CIDR
func isValidIPOrCIDR(s string) bool {
	// Try parsing as CIDR
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}

	// Try parsing as IP
	if net.ParseIP(s) != nil {
		return true
	}

	return false
}

// ValidateURL validates if a URL is accessible and returns valid IP/CIDR list
func (rm *Manager) ValidateURL(url string) (bool, string, int) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return false, err.Error(), 0
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("HTTP %d", resp.StatusCode), 0
	}

	validCount := 0
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		if isValidIPOrCIDR(line) {
			validCount++
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err.Error(), 0
	}

	if validCount == 0 {
		return false, "No valid IP/CIDR found", 0
	}

	return true, "", validCount
}
