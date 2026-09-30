package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/auth"
)

// GetAuthStatus возвращает, включен ли вход и выполнен ли он. Доступен без входа.
func (h *Handler) GetAuthStatus(w http.ResponseWriter, r *http.Request) {
	status := h.auth.Status()
	authenticated := h.auth.Authenticated(r)

	// Логин показывается только вошедшему пользователю.
	username := ""

	if status.Enabled && authenticated {
		username = status.Username
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       status.Enabled,
		"authenticated": authenticated,
		"username":      username,
	})
}

// Login проверяет логин и пароль и устанавливает cookie сессии.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if !decodeJSON(w, r, &req) {
		return
	}

	client := auth.ClientIP(r)

	token, err := h.auth.Login(req.Username, req.Password, client)

	var rateErr *auth.RateLimitError

	switch {
	case errors.As(err, &rateErr):
		writeJSONError(w, http.StatusTooManyRequests, err.Error())

	case errors.Is(err, auth.ErrInvalidCredentials):
		log.Printf("Login failed for %q from %s", req.Username, client)
		writeJSONError(w, http.StatusUnauthorized, err.Error())

	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())

	default:
		auth.SetSessionCookie(w, r, token)
		writeSuccess(w, "Вход выполнен")
	}
}

// Logout удаляет cookie сессии.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, r)
	writeSuccess(w, "Выход выполнен")
}

// GetAuthSettings возвращает настройки входа в панель.
func (h *Handler) GetAuthSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.auth.Status())
}

// UpdateAuthSettings включает, меняет или выключает вход в панель по логину и паролю.
func (h *Handler) UpdateAuthSettings(w http.ResponseWriter, r *http.Request) {
	var req auth.Update

	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.auth.Update(req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())

		return
	}

	// Ключ сессий сменился: текущий пользователь получает новую сессию, чтобы не выйти из панели.
	if req.Enabled {
		log.Printf("Panel login enabled for %q", h.auth.Status().Username)
		auth.SetSessionCookie(w, r, h.auth.NewSession())
		writeSuccess(w, "Вход по логину и паролю включен")

		return
	}

	log.Printf("Panel login disabled")
	auth.ClearSessionCookie(w, r)
	writeSuccess(w, "Вход по паролю выключен: панель открыта без аутентификации")
}
