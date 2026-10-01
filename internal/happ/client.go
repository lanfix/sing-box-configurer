package happ

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Параметры устройства, которое имитируется при запросе подписки.
const (
	deviceUserAgent = "Happ/3.3.0/ios CFNetwork/3826.500.131 Darwin/24.5.0"
	deviceOS        = "iOS"
	deviceOSVersion = "18.5"
	deviceModel     = "iPhone15,3"

	// Максимальный размер ответа подписки.
	maxSubscriptionSize = 8 << 20
)

// ProfileInfo содержит метаданные подписки из заголовков ответа.
type ProfileInfo struct {
	Title          string     `json:"title"`
	Announce       string     `json:"announce"`
	SupportURL     string     `json:"support_url"`
	WebPageURL     string     `json:"web_page_url"`
	Upload         int64      `json:"upload"`
	Download       int64      `json:"download"`
	Total          int64      `json:"total"`
	Expire         *time.Time `json:"expire,omitempty"`
	UpdateInterval int        `json:"update_interval_hours"`
}

// Subscription содержит ответ сервера подписки.
type Subscription struct {
	Info ProfileInfo
	Body []byte
}

// Client загружает подписки, представляясь клиентом Happ.
type Client struct {
	httpClient *http.Client
}

// NewClient создает клиент подписок.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Fetch загружает подписку по url от имени устройства с идентификатором hwid.
func (c *Client) Fetch(ctx context.Context, url, hwid string) (*Subscription, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("User-Agent", deviceUserAgent)
	req.Header.Set("X-HWID", hwid)
	req.Header.Set("X-Device-OS", deviceOS)
	req.Header.Set("X-Ver-OS", deviceOSVersion)
	req.Header.Set("X-Device-Model", deviceModel)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionSize))
	if err != nil {
		return nil, fmt.Errorf("cannot read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	return &Subscription{
		Info: parseProfileInfo(resp.Header),
		Body: body,
	}, nil
}

// parseProfileInfo извлекает метаданные подписки из заголовков ответа.
func parseProfileInfo(header http.Header) ProfileInfo {
	info := ProfileInfo{
		Title:          decodeHeaderValue(header.Get("Profile-Title")),
		Announce:       decodeHeaderValue(header.Get("Announce")),
		SupportURL:     SafeURL(header.Get("Support-Url")),
		WebPageURL:     SafeURL(header.Get("Profile-Web-Page-Url")),
		Upload:         0,
		Download:       0,
		Total:          0,
		Expire:         nil,
		UpdateInterval: 0,
	}

	if interval, err := strconv.Atoi(strings.TrimSpace(header.Get("Profile-Update-Interval"))); err == nil {
		info.UpdateInterval = interval
	}

	// Формат: upload=0; download=120549883407; total=0; expire=1790970491
	for part := range strings.SplitSeq(header.Get("Subscription-Userinfo"), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}

		number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}

		switch strings.TrimSpace(key) {
		case "upload":
			info.Upload = number

		case "download":
			info.Download = number

		case "total":
			info.Total = number

		case "expire":
			if number > 0 {
				info.Expire = new(time.Unix(number, 0).UTC())
			}
		}
	}

	return info
}

// SafeURL возвращает url, только если это ссылка http или https. Ссылки приходят от сервера подписки
// и показываются в интерфейсе: javascript: или data: в них выполнили бы чужой код в панели (XSS).
func SafeURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}

	return parsed.String()
}

// decodeHeaderValue декодирует значения заголовков в формате "base64:...".
func decodeHeaderValue(value string) string {
	encoded, ok := strings.CutPrefix(value, "base64:")
	if !ok {
		return value
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return value
	}

	return string(decoded)
}

// truncate обрезает строку до limit байт.
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit] + "..."
}
