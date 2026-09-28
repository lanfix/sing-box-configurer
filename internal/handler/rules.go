package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/rules"
)

// ruleTypes — допустимые типы правил.
var ruleTypes = []string{"domain", "domain_suffix", "ip", "cidr"}

// GetRules возвращает правила и количество неприменённых изменений.
func (h *Handler) GetRules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"rules":         h.rulesManager.GetRules(),
		"pending_count": h.rulesManager.GetPendingCount(),
	})
}

// AddRule добавляет правило.
func (h *Handler) AddRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type        string `json:"type"`
		Value       string `json:"value"`
		Description string `json:"description"`
		Group       string `json:"group"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	req.Value = strings.TrimSpace(req.Value)

	if req.Value == "" || req.Group == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужны значение и группа")

		return
	}

	if !slices.Contains(ruleTypes, req.Type) {
		writeJSONError(w, http.StatusBadRequest, "Тип правила должен быть domain, domain_suffix, ip или cidr")

		return
	}

	rule := rules.Rule{
		ID:          uuid.NewString(),
		Type:        req.Type,
		Value:       req.Value,
		Description: req.Description,
		Group:       req.Group,
	}

	if err := h.rulesManager.AddRule(rule); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"rule":    rule,
	})
}

// AddRuleBulk добавляет несколько правил одного типа (по одному значению на строку).
func (h *Handler) AddRuleBulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type        string `json:"type"`
		Values      string `json:"values"`
		Description string `json:"description"`
		Group       string `json:"group"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.Values) == "" || req.Group == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужны значения и группа")

		return
	}

	if !slices.Contains(ruleTypes, req.Type) {
		writeJSONError(w, http.StatusBadRequest, "Тип правила должен быть domain, domain_suffix, ip или cidr")

		return
	}

	result, err := h.rulesManager.AddRuleBulk(req.Type, req.Values, req.Description, req.Group)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// EditRule меняет описание и группу правила.
func (h *Handler) EditRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Group       string `json:"group"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if req.ID == "" || req.Group == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужны ID правила и группа")

		return
	}

	if err := h.rulesManager.EditRule(req.ID, req.Description, req.Group); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Правило обновлено")
}

// DeleteRule помечает правило на удаление.
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужен ID правила")

		return
	}

	if err := h.rulesManager.DeleteRule(req.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "Правило удалено")
}

// ApplyRules применяет изменения правил: rule-set-ы групп обновятся в sing-box без перезапуска.
func (h *Handler) ApplyRules(w http.ResponseWriter, _ *http.Request) {
	if err := h.rulesManager.ApplyRules(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "Правила применены")
}

// GetRuleSetByGroupKind возвращает обработчик, отдающий rule-set группы с доменами, IP или всеми значениями.
func (h *Handler) GetRuleSetByGroupKind(kind rules.RuleSetKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupName := r.URL.Query().Get("group")
		if groupName == "" {
			http.Error(w, "Group parameter is required", http.StatusBadRequest)

			return
		}

		ruleSet, err := h.rulesManager.GetRuleSetByGroup(groupName, kind)

		writeRuleSet(w, r, ruleSet, err)
	}
}

// GetBypassRuleSet возвращает rule-set системной группы bypass (мимо туннеля).
func (h *Handler) GetBypassRuleSet(w http.ResponseWriter, r *http.Request) {
	ruleSet, err := h.rulesManager.GetBypassRuleSet()

	writeRuleSet(w, r, ruleSet, err)
}

// writeRuleSet отправляет ruleset для sing-box. Пока набор не готов, отвечает 503: sing-box оставит
// прежний (закэшированный) набор, а при первом запуске без кэша не стартует до готовности набора.
//
// Ответ помечается ETag. Если набор не изменился, отвечает 304: sing-box не перезагружает набор, а для
// route_exclude_address_set не перезаписывает nftables-набор (иначе он пересоздается каждые update_interval).
func writeRuleSet(w http.ResponseWriter, r *http.Request, ruleSet rules.SingBoxRuleSet, err error) {
	if errors.Is(err, rules.ErrRuleSetNotReady) {
		log.Printf("Rule-set requested before it is ready: %v", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)

		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	body, err := json.Marshal(ruleSet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`

	w.Header().Set("ETag", etag)

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(body)
}

// GetGroups возвращает системные и пользовательские группы.
func (h *Handler) GetGroups(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": h.rulesManager.GetAllGroups(),
	})
}

// groupRequest — тело запросов добавления и редактирования группы.
type groupRequest struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	DefaultOutbound string `json:"default_outbound"`
	DNSServer       string `json:"dns_server"`
}

// validateGroupDNSServer проверяет, что DNS-сервер группы существует.
func (h *Handler) validateGroupDNSServer(dnsServer string) error {
	if dnsServer != "" && !slices.Contains(h.dnsManager.ServerTags(), dnsServer) {
		return fmt.Errorf("DNS-сервер %s не найден", dnsServer)
	}

	return nil
}

// AddGroup добавляет группу.
func (h *Handler) AddGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.validateGroupDNSServer(req.DNSServer); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	group := rules.Group{
		Name:            req.Name,
		Description:     req.Description,
		DefaultOutbound: req.DefaultOutbound,
		DNSServer:       req.DNSServer,
	}

	if err := h.rulesManager.AddGroup(group); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Группа добавлена")
}

// EditGroup меняет описание, outbound по умолчанию и DNS-сервер группы.
func (h *Handler) EditGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.validateGroupDNSServer(req.DNSServer); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	if err := h.rulesManager.EditGroup(req.Name, req.Description, req.DefaultOutbound, req.DNSServer); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Группа обновлена")
}

// DeleteGroup удаляет пустую группу. Группу, selector которой используется DNS-сервером, удалить нельзя.
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest

	if !decodeJSON(w, r, &req) {
		return
	}

	selector := render.SelectorTag(req.Name)

	for _, server := range h.dnsManager.Get().Servers {
		if server.Detour == selector {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("selector %s используется как detour DNS-сервера %s", selector, server.Tag))

			return
		}
	}

	if err := h.rulesManager.DeleteGroup(req.Name); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "Группа удалена")
}

// GetURLSources возвращает URL-источники.
func (h *Handler) GetURLSources(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"url_sources": h.rulesManager.GetURLSources(),
	})
}

// AddURLSource добавляет URL-источник.
func (h *Handler) AddURLSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL         string `json:"url"`
		Description string `json:"description"`
		Interval    int    `json:"interval"`
		Group       string `json:"group"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.URL) == "" || req.Group == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужны URL и группа")

		return
	}

	if req.Interval <= 0 {
		req.Interval = 60
	}

	source := rules.URLSource{
		ID:          uuid.NewString(),
		URL:         strings.TrimSpace(req.URL),
		Description: req.Description,
		Interval:    req.Interval,
		Group:       req.Group,
	}

	if err := h.rulesManager.AddURLSource(source); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "URL-источник добавлен")
}

// EditURLSource меняет описание и группу URL-источника.
func (h *Handler) EditURLSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Group       string `json:"group"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if req.ID == "" || req.Group == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужны ID источника и группа")

		return
	}

	if err := h.rulesManager.EditURLSource(req.ID, req.Description, req.Group); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "URL-источник обновлен")
}

// DeleteURLSource помечает URL-источник на удаление.
func (h *Handler) DeleteURLSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.rulesManager.DeleteURLSource(req.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "URL-источник удален")
}

// RefreshURLSource сразу загружает правила URL-источника, не дожидаясь интервала обновления.
func (h *Handler) RefreshURLSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.rulesManager.RefreshURLSource(req.ID)

	switch {
	case errors.Is(err, rules.ErrURLSourceNotFound):
		writeJSONError(w, http.StatusNotFound, "Источник не найден")

	case errors.Is(err, rules.ErrURLSourceNotApplied):
		writeJSONError(w, http.StatusConflict, "Источник ещё не применён")

	case err != nil:
		// Ошибка загрузки уже сохранена в статусе источника и видна в таблице.
		writeJSONError(w, http.StatusBadGateway, err.Error())

	default:
		writeSuccess(w, "Источник загружен")
	}
}

// ApplyURLSources применяет изменения URL-источников и запускает их периодическую загрузку.
func (h *Handler) ApplyURLSources(w http.ResponseWriter, _ *http.Request) {
	if err := h.rulesManager.ApplyURLSources(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	writeSuccess(w, "URL-источники применены")
}

// ValidateURLSource загружает список по URL и возвращает количество найденных правил.
func (h *Handler) ValidateURLSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.URL) == "" {
		writeJSONError(w, http.StatusBadRequest, "Нужен URL")

		return
	}

	ruleSet, err := h.rulesManager.GatherRuleSetFromURL(strings.TrimSpace(req.URL))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"valid": false,
			"error": err.Error(),
			"count": 0,
		})

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid": true,
		"error": "",
		"count": ruleSet.Total(),
	})
}

// GetURLSourceRules возвращает загруженные правила URL-источника.
func (h *Handler) GetURLSourceRules(w http.ResponseWriter, r *http.Request) {
	ruleSet, err := h.rulesManager.GetURLSourceRuleSet(r.URL.Query().Get("id"))
	if err != nil {
		// Правила хранятся в памяти и появляются только после успешной загрузки источника.
		writeJSONError(w, http.StatusNotFound, "Правила источника ещё не загружены: последняя загрузка не удалась или ещё не выполнялась")

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cidrList":       ruleSet.CidrList,
		"domains":        ruleSet.Domains,
		"domainSuffixes": ruleSet.DomainSuffixes,
	})
}
