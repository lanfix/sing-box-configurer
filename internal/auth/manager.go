// Package auth закрывает веб-интерфейс и API конфигуратора логином и паролем. Учетные данные хранятся
// в разделе "auth" файла app.json (пароль — хэшем PBKDF2), сессия — cookie, подписанная HMAC.
// Аутентификация выключена, пока пользователь не задаст логин и пароль.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

const (
	// CookieName — cookie сессии.
	CookieName = "sbc_session"

	// SessionTTL — срок действия сессии.
	SessionTTL = 30 * 24 * time.Hour

	minPasswordLength = 8
	maxUsernameLength = 64
	maxPasswordLength = 256
)

var (
	// ErrInvalidCredentials — неверный логин или пароль.
	ErrInvalidCredentials = errors.New("неверный логин или пароль")

	// ErrInvalidCurrentPassword — неверный текущий пароль при изменении учетных данных.
	ErrInvalidCurrentPassword = errors.New("неверный текущий пароль")
)

// RateLimitError — слишком много неудачных попыток входа.
type RateLimitError struct {
	RetryAfter time.Duration
}

// Error возвращает текст ошибки.
func (e *RateLimitError) Error() string {
	minutes := int(e.RetryAfter.Minutes()) + 1

	return fmt.Sprintf("слишком много неудачных попыток, повторите через %d мин", minutes)
}

// Credentials — раздел "auth" файла app.json.
type Credentials struct {
	Enabled      bool   `json:"enabled"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`

	// SessionKey — ключ подписи cookie сессий. Меняется при изменении учетных данных,
	// поэтому прежние сессии перестают действовать.
	SessionKey string `json:"session_key"`
}

// Status — состояние аутентификации для интерфейса.
type Status struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
}

// Update — изменение учетных данных из интерфейса.
type Update struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`

	// Password — новый пароль. Пустой — оставить прежний (если вход уже включен).
	Password string `json:"password"`

	// CurrentPassword — текущий пароль, обязателен, если вход уже включен.
	CurrentPassword string `json:"current_password"`
}

// appDataSection описывает раздел app.json, которым владеет менеджер.
type appDataSection struct {
	Auth *Credentials `json:"auth"`
}

// Manager хранит учетные данные и проверяет сессии.
type Manager struct {
	appData *appdata.File
	limiter *limiter

	mu   sync.RWMutex
	data Credentials
}

// NewManager загружает учетные данные из app.json.
func NewManager(appData *appdata.File) (*Manager, error) {
	m := &Manager{
		appData: appData,
		limiter: newLimiter(),
		mu:      sync.RWMutex{},
		data: Credentials{
			Enabled:      false,
			Username:     "",
			PasswordHash: "",
			SessionKey:   "",
		},
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read auth settings: %w", err)
	}

	if section.Auth != nil {
		m.data = *section.Auth
	}

	// Включенный вход без пароля или ключа сессий — поврежденные данные: панель остается закрытой,
	// пока учетные данные не сбросят командой auth reset.
	if m.data.Enabled && (m.data.Username == "" || m.data.PasswordHash == "" || m.data.SessionKey == "") {
		return nil, errors.New("auth settings in app data are incomplete: reset them with 'sing-box-configurer auth reset'")
	}

	return m, nil
}

// Status возвращает, включен ли вход, и логин.
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return Status{
		Enabled:  m.data.Enabled,
		Username: m.data.Username,
	}
}

// Enabled возвращает true, если панель закрыта логином и паролем.
func (m *Manager) Enabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.data.Enabled
}

// Update меняет учетные данные. Если вход уже включен, нужен текущий пароль.
func (m *Manager) Update(update Update) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.data.Enabled && !VerifyPassword(update.CurrentPassword, m.data.PasswordHash) {
		return ErrInvalidCurrentPassword
	}

	if !update.Enabled {
		return m.saveLocked(disabledCredentials())
	}

	username := strings.TrimSpace(update.Username)

	if err := validateUsername(username); err != nil {
		return err
	}

	passwordHash := m.data.PasswordHash

	switch {
	case update.Password != "":
		if err := validatePassword(update.Password); err != nil {
			return err
		}

		hash, err := HashPassword(update.Password)
		if err != nil {
			return err
		}

		passwordHash = hash

	case !m.data.Enabled:
		return errors.New("задайте пароль")
	}

	return m.saveCredentialsLocked(username, passwordHash)
}

// Set включает вход с логином username и паролем password (команда auth set).
func (m *Manager) Set(username, password string) error {
	username = strings.TrimSpace(username)

	if err := validateUsername(username); err != nil {
		return err
	}

	if err := validatePassword(password); err != nil {
		return err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.saveCredentialsLocked(username, hash)
}

// Reset выключает вход и удаляет учетные данные (команда auth reset).
func (m *Manager) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.saveLocked(disabledCredentials())
}

// Login проверяет логин и пароль клиента client (IP-адрес) и возвращает токен сессии.
func (m *Manager) Login(username, password, client string) (string, error) {
	if wait := m.limiter.RetryAfter(client); wait > 0 {
		return "", &RateLimitError{
			RetryAfter: wait,
		}
	}

	m.mu.RLock()
	data := m.data
	m.mu.RUnlock()

	if !data.Enabled {
		return "", errors.New("вход по паролю не включен")
	}

	// Пароль проверяется и при неверном логине: время ответа не выдает, существует ли логин.
	usernameOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(data.Username)) == 1
	passwordOK := VerifyPassword(password, data.PasswordHash)

	if !usernameOK || !passwordOK {
		m.limiter.Fail(client)

		return "", ErrInvalidCredentials
	}

	m.limiter.Reset(client)

	return m.newToken(data, time.Now().Add(SessionTTL)), nil
}

// NewSession возвращает токен сессии текущего пользователя (после изменения учетных данных
// прежний токен недействителен, и пользователь получает новый).
func (m *Manager) NewSession() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.newToken(m.data, time.Now().Add(SessionTTL))
}

// Valid проверяет токен сессии.
func (m *Manager) Valid(token string) bool {
	m.mu.RLock()
	data := m.data
	m.mu.RUnlock()

	if !data.Enabled || token == "" {
		return false
	}

	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}

	if !hmac.Equal([]byte(signature), []byte(sign(data.SessionKey, payload))) {
		return false
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return false
	}

	username, expires, ok := strings.Cut(string(raw), "|")
	if !ok || username != data.Username {
		return false
	}

	expiresAt, err := strconv.ParseInt(expires, 10, 64)

	return err == nil && time.Now().Unix() < expiresAt
}

// Authenticated проверяет cookie сессии запроса. Если вход выключен, любой запрос аутентифицирован.
func (m *Manager) Authenticated(r *http.Request) bool {
	if !m.Enabled() {
		return true
	}

	cookie, err := r.Cookie(CookieName)

	return err == nil && m.Valid(cookie.Value)
}

// newToken возвращает токен вида base64(username|expires).hmac.
func (m *Manager) newToken(data Credentials, expires time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(data.Username + "|" + strconv.FormatInt(expires.Unix(), 10)))

	return payload + "." + sign(data.SessionKey, payload)
}

// saveCredentialsLocked включает вход с новым ключом сессий. Вызывается под блокировкой.
func (m *Manager) saveCredentialsLocked(username, passwordHash string) error {
	key, err := randomKey()
	if err != nil {
		return err
	}

	return m.saveLocked(Credentials{
		Enabled:      true,
		Username:     username,
		PasswordHash: passwordHash,
		SessionKey:   key,
	})
}

// saveLocked сохраняет учетные данные в app.json. Вызывается под блокировкой.
func (m *Manager) saveLocked(data Credentials) error {
	section := appDataSection{
		Auth: &data,
	}

	if err := m.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save auth settings: %w", err)
	}

	m.data = data

	return nil
}

// disabledCredentials возвращает учетные данные с выключенным входом.
func disabledCredentials() Credentials {
	return Credentials{
		Enabled:      false,
		Username:     "",
		PasswordHash: "",
		SessionKey:   "",
	}
}

// validateUsername проверяет логин.
func validateUsername(username string) error {
	if username == "" {
		return errors.New("задайте логин")
	}

	if utf8.RuneCountInString(username) > maxUsernameLength || strings.ContainsAny(username, "|\r\n") {
		return fmt.Errorf("логин должен быть не длиннее %d символов и не содержать | и переводов строк", maxUsernameLength)
	}

	return nil
}

// validatePassword проверяет пароль.
func validatePassword(password string) error {
	length := utf8.RuneCountInString(password)

	if length < minPasswordLength {
		return fmt.Errorf("пароль должен быть не короче %d символов", minPasswordLength)
	}

	if length > maxPasswordLength {
		return fmt.Errorf("пароль должен быть не длиннее %d символов", maxPasswordLength)
	}

	return nil
}

// sign возвращает HMAC-SHA256 payload в base64.
func sign(key, payload string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// randomKey возвращает случайный ключ длиной 32 байта в hex.
func randomKey() (string, error) {
	buf := make([]byte, 32)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot generate session key: %w", err)
	}

	return hex.EncodeToString(buf), nil
}
