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

	// Проверяем, что группа существует.
	if !rm.groupExists(source.Group) {
		return fmt.Errorf("группа %s не существует", source.Group)
	}

	source.Applied = false
	source.CreatedAt = time.Now()
	source.LastStatus = "pending"
	rm.data.URLSources = append(rm.data.URLSources, source)

	return rm.save()
}

// EditURLSource обновляет параметры URL источника.
func (rm *Manager) EditURLSource(id string, description string, group string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Проверяем, что группа существует.
	if !rm.groupExists(group) {
		return fmt.Errorf("группа %s не существует", group)
	}

	// Находим источник.
	found := false

	for i := range rm.data.URLSources {
		if rm.data.URLSources[i].ID == id {
			rm.data.URLSources[i].Description = description
			rm.data.URLSources[i].Group = group
			found = true

			break
		}
	}

	if !found {
		return fmt.Errorf("источник с ID %s не найден", id)
	}

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

// GetURLSourceRuleSet возвращает набор правил для указанного источника.
func (rm *Manager) GetURLSourceRuleSet(sourceID string) (*RuleSet, error) {
	rm.urlRulesMu.RLock()
	defer rm.urlRulesMu.RUnlock()

	if ruleSet, exists := rm.urlRules[sourceID]; exists {
		return &ruleSet, nil
	}

	return nil, fmt.Errorf("cannot find rule set for source %s", sourceID)
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

	rm.fetchRulesFromSource(source.ID)

	ticker := time.NewTicker(time.Duration(source.Interval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			rm.fetchRulesFromSource(source.ID)
		}
	}
}

// fetchRulesFromSource получает правила из источника и сохраняет их.
func (rm *Manager) fetchRulesFromSource(sourceID string) {
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

	ruleSet := RuleSet{
		CidrList:       make([]string, 0),
		Domains:        make([]string, 0),
		DomainSuffixes: make([]string, 0),
	}

	handler := func(row string) error {
		return rowHandler(row, &ruleSet)
	}

	if err := rm.scanAndHandleRowsFromURL(source.URL, handler); err != nil {
		log.Printf("Error scanning and handling URL source %s: %s", source.URL, err)

		return
	}

	rm.urlRulesMu.Lock()
	rm.urlRules[sourceID] = ruleSet
	rm.urlRulesMu.Unlock()

	rm.updateURLSourceStatus(sourceID, "success", "", ruleSet.Total())

	log.Printf("Successfully fetched %d items from URL source: %s", ruleSet.Total(), source.Description)
}

// updateURLSourceStatus updates the status of a URL source
func (rm *Manager) updateURLSourceStatus(sourceID, status, errorMsg string, count uint64) {
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

	if err := rm.save(); err != nil {
		log.Printf("Error updating URL source status for %s: %s", sourceID, err)
	}

}

type RuleSet struct {
	CidrList       []string
	Domains        []string
	DomainSuffixes []string
}

// Total возвращает общее количество правил.
func (rs *RuleSet) Total() uint64 {
	return uint64(len(rs.CidrList) + len(rs.Domains) + len(rs.DomainSuffixes))
}

// GatherRuleSetFromURL запрашивает данные по url и собирает их в структуру.
func (rm *Manager) GatherRuleSetFromURL(url string) (*RuleSet, error) {
	ruleSet := RuleSet{
		CidrList:       make([]string, 0),
		Domains:        make([]string, 0),
		DomainSuffixes: make([]string, 0),
	}

	handler := func(row string) error {
		return rowHandler(row, &ruleSet)
	}

	if err := rm.scanAndHandleRowsFromURL(url, handler); err != nil {
		return nil, fmt.Errorf("cannot scan and handle rows from url: %w", err)
	}

	return &ruleSet, nil
}

func (rm *Manager) scanAndHandleRowsFromURL(url string, f func(row string) error) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: rm.sourceListsProxy,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("cannot get URL source %s: %v", url, err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code is %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		row := strings.TrimSpace(scanner.Text())

		if err := f(row); err != nil {
			return fmt.Errorf("cannot handle row %s: %v", row, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("cannot scan rows: %v", err)
	}

	return nil
}

func rowHandler(row string, rs *RuleSet) error {
	row = strings.TrimSpace(row)

	if row == "" || strings.HasPrefix(row, "#") || strings.HasPrefix(row, "//") {
		return nil
	}

	// Пропускаем строки для конкретных регионов.
	if strings.Contains(row, "@") {
		return nil
	}

	// Обработка префикса full: (точное совпадение).
	if strings.HasPrefix(row, "full:") {
		domain := strings.TrimPrefix(row, "full:")
		domain = strings.TrimSpace(domain)

		if domain != "" {
			rs.Domains = append(rs.Domains, domain)

			return nil
		}
	}

	// Обработка CIDR префикса.
	if strings.Contains(row, "/") {
		if _, _, err := net.ParseCIDR(row); err == nil {
			rs.CidrList = append(rs.CidrList, row)
		}

		// Выходим даже если CIDR не добавился в список.
		return nil
	}

	// Обработка одиночного IP адреса.
	if ip := net.ParseIP(row); ip != nil {
		rs.CidrList = append(rs.CidrList, row+"/32")

		return nil
	}

	// Начинается с точки - это суффикс (все поддомены).
	// Пример: ".google.com" -> все поддомены google.com.
	if strings.HasPrefix(row, ".") {
		suffix := strings.TrimPrefix(row, ".")
		suffix = strings.TrimSpace(suffix)

		if suffix != "" {
			rs.DomainSuffixes = append(rs.DomainSuffixes, suffix)
		}

		// Выходим даже есть домен не добавился.
		return nil
	}

	// Обычный домен - добавляем как суффикс для поддоменов.
	if strings.Contains(row, ".") {
		rs.DomainSuffixes = append(rs.DomainSuffixes, row)

		return nil
	}

	return nil
}
