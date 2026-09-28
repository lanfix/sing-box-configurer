package singboxclashapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ClashAPI struct {
	baseUrl string
	// secret возвращает текущий секрет Clash API: он меняется при применении нового конфига sing-box.
	secret func() string
	client *http.Client
	// streamClient — для потоковых эндпоинтов (/traffic), у которых не должно
	// быть общего дедлайна на весь ответ.
	streamClient *http.Client
}

// NewClashAPI создает клиент Clash API по адресу baseUrl. secret вызывается перед каждым запросом.
func NewClashAPI(baseUrl string, secret func() string) *ClashAPI {
	if strings.HasSuffix(baseUrl, "/") {
		baseUrl = baseUrl[:len(baseUrl)-1]
	}

	client := &http.Client{
		Timeout: time.Second * 15,
	}

	streamClient := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: time.Second * 10,
		},
	}

	return &ClashAPI{
		baseUrl:      baseUrl,
		secret:       secret,
		client:       client,
		streamClient: streamClient,
	}
}

func (api *ClashAPI) do(req *http.Request) (*http.Response, error) {
	return api.client.Do(req)
}

func (api *ClashAPI) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, api.baseUrl+path, body)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if secret := api.secret(); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	return api.do(req)
}

func (api *ClashAPI) ReloadConfig() error {
	resp, err := api.doRequest(http.MethodPut, "/configs?force=true", nil)
	if err != nil {
		return fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code is not ok: %d", resp.StatusCode)
	}

	return nil
}

// DelayHistory — точка истории замеров задержки прокси.
type DelayHistory struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

// Proxy — описание прокси или прокси-группы из Clash API.
type Proxy struct {
	Type    string         `json:"type"`
	Name    string         `json:"name"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	History []DelayHistory `json:"history"`
	UDP     bool           `json:"udp"`
}

type proxiesResponse struct {
	Proxies map[string]Proxy `json:"proxies"`
}

// GetProxies возвращает все прокси и прокси-группы, известные sing-box.
func (api *ClashAPI) GetProxies() (map[string]Proxy, error) {
	resp, err := api.doRequest(http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	var parsed proxiesResponse

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("cannot decode response: %w", err)
	}

	return parsed.Proxies, nil
}

// SelectProxy переключает активный элемент selector-группы.
func (api *ClashAPI) SelectProxy(group, name string) error {
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return fmt.Errorf("cannot marshal request: %w", err)
	}

	path := "/proxies/" + url.PathEscape(group)

	resp, err := api.doRequest(http.MethodPut, path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

type delayResponse struct {
	Delay int `json:"delay"`
}

// TestProxyDelay замеряет задержку одного прокси через указанный URL.
func (api *ClashAPI) TestProxyDelay(name, testURL string, timeoutMs int) (int, error) {
	query := url.Values{}
	query.Set("url", testURL)
	query.Set("timeout", fmt.Sprintf("%d", timeoutMs))

	path := "/proxies/" + url.PathEscape(name) + "/delay?" + query.Encode()

	resp, err := api.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return 0, fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	var parsed delayResponse

	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, fmt.Errorf("cannot decode response: %w", err)
	}

	return parsed.Delay, nil
}

// TestGroupDelay замеряет задержку всех элементов прокси-группы разом.
// Возвращает карту "имя прокси -> задержка в мс" (0 означает таймаут/ошибку).
func (api *ClashAPI) TestGroupDelay(group, testURL string, timeoutMs int) (map[string]int, error) {
	query := url.Values{}
	query.Set("url", testURL)
	query.Set("timeout", fmt.Sprintf("%d", timeoutMs))

	path := "/group/" + url.PathEscape(group) + "/delay?" + query.Encode()

	resp, err := api.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	result := map[string]int{}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("cannot decode response: %w", err)
	}

	return result, nil
}

// ConnectionMetadata — сведения об одном соединении.
type ConnectionMetadata struct {
	Network         string `json:"network"`
	Host            string `json:"host"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	SourceIP        string `json:"sourceIP"`
}

// Connection — активное соединение из Clash API.
type Connection struct {
	Chains   []string           `json:"chains"`
	Rule     string             `json:"rule"`
	Upload   int64              `json:"upload"`
	Download int64              `json:"download"`
	Start    string             `json:"start"`
	Metadata ConnectionMetadata `json:"metadata"`
}

// Connections — снимок трафика и активных соединений.
type Connections struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Memory        int64        `json:"memory"`
	Connections   []Connection `json:"connections"`
}

// GetConnections возвращает суммарный трафик, память и список соединений.
func (api *ClashAPI) GetConnections() (*Connections, error) {
	resp, err := api.doRequest(http.MethodGet, "/connections", nil)
	if err != nil {
		return nil, fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	var parsed Connections

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("cannot decode response: %w", err)
	}

	return &parsed, nil
}

type rulesResponse struct {
	Rules []struct{} `json:"rules"`
}

// GetRulesCount возвращает количество активных правил маршрутизации.
func (api *ClashAPI) GetRulesCount() (int, error) {
	resp, err := api.doRequest(http.MethodGet, "/rules", nil)
	if err != nil {
		return 0, fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return 0, fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	var parsed rulesResponse

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("cannot decode response: %w", err)
	}

	return len(parsed.Rules), nil
}

type versionResponse struct {
	Version string `json:"version"`
}

// GetVersion возвращает версию ядра sing-box.
func (api *ClashAPI) GetVersion() (string, error) {
	resp, err := api.doRequest(http.MethodGet, "/version", nil)
	if err != nil {
		return "", fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	var parsed versionResponse

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("cannot decode response: %w", err)
	}

	return parsed.Version, nil
}

// TrafficSample — мгновенная скорость в байтах за секунду.
type TrafficSample struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// StreamTraffic читает поток /traffic, вызывая onSample на каждое измерение.
// Возвращает управление только при обрыве потока или отмене контекста.
func (api *ClashAPI) StreamTraffic(ctx context.Context, onSample func(TrafficSample)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api.baseUrl+"/traffic", nil)
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}

	if secret := api.secret(); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	resp, err := api.streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	decoder := json.NewDecoder(resp.Body)

	for {
		var sample TrafficSample

		if err := decoder.Decode(&sample); err != nil {
			return fmt.Errorf("cannot decode sample: %w", err)
		}

		onSample(sample)
	}
}
