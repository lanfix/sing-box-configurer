package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
)

// urlTestView — urltest с подобранными участниками.
type urlTestView struct {
	outbound.URLTest

	Members []outbound.Member `json:"members"`
	Error   string            `json:"error,omitempty"`
}

// profileView — профиль подписки, который можно выбрать источником urltest-а.
type profileView struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

// GetURLTests возвращает urltest-ы с участниками, а также outbound-ы и профили подписок для формы.
func (h *Handler) GetURLTests(w http.ResponseWriter, _ *http.Request) {
	candidates, err := h.singBox.Candidates()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	urlTests := h.outboundManager.ListURLTests()
	views := make([]urlTestView, 0, len(urlTests))

	for _, urlTest := range urlTests {
		view := urlTestView{
			URLTest: urlTest,
			Members: []outbound.Member{},
			Error:   "",
		}

		if members, resolveErr := urlTest.Resolve(candidates); resolveErr != nil {
			view.Error = resolveErr.Error()
		} else {
			view.Members = members
		}

		views = append(views, view)
	}

	profiles := make([]profileView, 0)

	for _, profile := range h.happManager.List() {
		profiles = append(profiles, profileView{
			Source: outbound.SourceHapp,
			ID:     profile.ID,
			Name:   profile.Name,
		})
	}

	for _, profile := range h.amneziaManager.List() {
		profiles = append(profiles, profileView{
			Source: outbound.SourceAmnezia,
			ID:     profile.ID,
			Name:   profile.Name,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"urltests":   views,
		"candidates": candidates,
		"profiles":   profiles,
	})
}

// PreviewURLTest подбирает участников urltest-а из тела запроса, ничего не сохраняя. Ошибка в выражениях
// фильтров возвращается в поле error ответа.
func (h *Handler) PreviewURLTest(w http.ResponseWriter, r *http.Request) {
	var req outbound.URLTest

	if !decodeJSON(w, r, &req) {
		return
	}

	candidates, err := h.singBox.Candidates()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())

		return
	}

	req.Normalize()

	members, err := req.Resolve(candidates)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"members": []outbound.Member{},
			"error":   err.Error(),
		})

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"members": members,
	})
}

// AddURLTest добавляет urltest.
func (h *Handler) AddURLTest(w http.ResponseWriter, r *http.Request) {
	var req outbound.URLTest

	if !decodeJSON(w, r, &req) {
		return
	}

	urlTest, err := h.outboundManager.AddURLTest(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	writeSuccess(w, "urltest "+urlTest.Tag+" добавлен")
}

// EditURLTest заменяет параметры urltest-а. Тег не меняется.
func (h *Handler) EditURLTest(w http.ResponseWriter, r *http.Request) {
	var req outbound.URLTest

	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.outboundManager.UpdateURLTest(req)

	switch {
	case errors.Is(err, outbound.ErrURLTestNotFound):
		writeJSONError(w, http.StatusNotFound, "urltest не найден")

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		writeSuccess(w, "urltest обновлен")
	}
}

// DeleteURLTest удаляет urltest. urltest, выбранный группой по умолчанию или указанный detour-ом
// DNS-сервера, удалить нельзя.
func (h *Handler) DeleteURLTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	urlTest, ok := h.outboundManager.GetURLTest(req.ID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "urltest не найден")

		return
	}

	if err := h.checkOutboundUnused(urlTest.Tag); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	err := h.outboundManager.DeleteURLTest(req.ID)

	switch {
	case errors.Is(err, outbound.ErrURLTestNotFound):
		writeJSONError(w, http.StatusNotFound, "urltest не найден")

	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, err.Error())

	default:
		writeSuccess(w, "urltest "+urlTest.Tag+" удален")
	}
}

// checkOutboundUnused проверяет, что outbound с тегом tag не выбран группами по умолчанию и не указан
// detour-ом DNS-серверов.
func (h *Handler) checkOutboundUnused(tag string) error {
	groups := make([]string, 0)

	for _, group := range h.rulesManager.GetGroups() {
		if group.DefaultOutbound == tag {
			groups = append(groups, group.Name)
		}
	}

	if len(groups) > 0 {
		return fmt.Errorf("%s выбран outbound-ом по умолчанию в группах: %s", tag, strings.Join(groups, ", "))
	}

	for _, server := range h.dnsManager.Get().Servers {
		if server.Detour == tag {
			return fmt.Errorf("%s используется как detour DNS-сервера %s", tag, server.Tag)
		}
	}

	return nil
}
