package dockercontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound возвращается, если docker-controller ответил 404 (нет контейнера или метода API).
var ErrNotFound = errors.New("not found")

type Provider struct {
	baseUrl string
	apiKey  string
	client  *http.Client

	// runClient используется для запуска контейнеров: контроллер может скачивать образ.
	runClient *http.Client
}

func NewProvider(baseUrl, apiKey string) *Provider {
	if strings.HasSuffix(baseUrl, "/") {
		baseUrl = baseUrl[:len(baseUrl)-1]
	}

	client := &http.Client{
		Timeout: time.Second * 30,
	}

	return &Provider{
		baseUrl: baseUrl,
		apiKey:  apiKey,
		client:  client,
		runClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// BaseURL возвращает адрес docker-controller.
func (p *Provider) BaseURL() string {
	return p.baseUrl
}

// APIKey возвращает ключ API docker-controller (передается updater-job).
func (p *Provider) APIKey() string {
	return p.apiKey
}

func (p *Provider) do(req *http.Request) (*http.Response, error) {
	return p.client.Do(req)
}

func (p *Provider) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, p.baseUrl+path, body)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)

	}

	log.Printf("Do request [%s] %s", req.Method, req.URL.String())

	req.Header.Set("Content-Type", "application/json")

	if p.apiKey != "" {
		req.Header.Set("X-API-Key", p.apiKey)
	}

	return p.do(req)
}

func (p *Provider) RestartContainersByLabels(labels map[string]string) error {
	requestBody := map[string]interface{}{
		"labels": labels,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("cannot marshal request: %w", err)
	}

	resp, err := p.doRequest(http.MethodPost, "/api/restart", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("cannot do request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("status code is %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("cannot decode response: %w", err)
	}

	success, _ := result["success"].(bool)
	if !success {
		errorMsg := "unknown error"

		if errVal, ok := result["error"].(string); ok {
			errorMsg = errVal
		}

		return fmt.Errorf("cannot restart containers: %s", errorMsg)
	}

	return nil
}

// RunRequest описывает запуск контейнера через docker-controller.
type RunRequest struct {
	Image         string            `json:"image"`
	Name          string            `json:"name"`
	Cmd           []string          `json:"cmd"`
	Env           []string          `json:"env"`
	Labels        map[string]string `json:"labels"`
	Binds         []string          `json:"binds"`
	Network       string            `json:"network"`
	RestartPolicy string            `json:"restart_policy"`
	MaxRetries    int               `json:"max_retries"`
	Pull          bool              `json:"pull"`
}

// Container описывает контейнер в ответах docker-controller.
type Container struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	Running    bool              `json:"running"`
	ExitCode   int               `json:"exit_code"`
	Error      string            `json:"error"`
	StartedAt  string            `json:"started_at"`
	FinishedAt string            `json:"finished_at"`
	Labels     map[string]string `json:"labels"`
	Networks   []string          `json:"networks"`
	Mounts     []Mount           `json:"mounts"`
	Restarting bool              `json:"restarting"`
}

// Mount описывает точку монтирования контейнера.
type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// Exited возвращает true, если контейнер завершился или ушел в цикл перезапусков.
func (c Container) Exited() bool {
	return c.Restarting || c.State == "exited" || c.State == "dead"
}

// ReplaceResult — результат замены образа контейнера.
type ReplaceResult struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}

// ContainerFilter — фильтры списка контейнеров. Пустые поля не учитываются.
type ContainerFilter struct {
	ID     string
	Name   string
	Labels map[string]string
}

// RunContainer запускает контейнер.
func (p *Provider) RunContainer(ctx context.Context, request RunRequest) (*Container, error) {
	var result Container

	if err := p.call(ctx, p.runClient, http.MethodPost, "/api/containers/run", request, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListContainers возвращает контейнеры (включая остановленные) по фильтрам.
func (p *Provider) ListContainers(ctx context.Context, filter ContainerFilter) ([]Container, error) {
	query := url.Values{}

	if filter.ID != "" {
		query.Set("id", filter.ID)
	}

	if filter.Name != "" {
		query.Set("name", filter.Name)
	}

	for key, value := range filter.Labels {
		query.Add("label", key+"="+value)
	}

	var result struct {
		Containers []Container `json:"containers"`
	}

	if err := p.call(ctx, p.client, http.MethodGet, "/api/containers?"+query.Encode(), nil, &result); err != nil {
		return nil, err
	}

	return result.Containers, nil
}

// ContainerLogs возвращает последние tail строк логов контейнера ("all" — все).
func (p *Provider) ContainerLogs(ctx context.Context, id, tail string) (string, error) {
	var result struct {
		Logs string `json:"logs"`
	}

	path := fmt.Sprintf("/api/containers/%s/logs?tail=%s", url.PathEscape(id), url.QueryEscape(tail))

	if err := p.call(ctx, p.client, http.MethodGet, path, nil, &result); err != nil {
		return "", err
	}

	return result.Logs, nil
}

// DeleteContainer удаляет контейнер, созданный через RunContainer.
func (p *Provider) DeleteContainer(ctx context.Context, id string) error {
	return p.call(ctx, p.client, http.MethodDelete, "/api/containers/"+url.PathEscape(id), nil, nil)
}

// GetContainer возвращает контейнер по имени или ID. Если контейнера нет, возвращается ErrNotFound.
func (p *Provider) GetContainer(ctx context.Context, nameOrID string) (*Container, error) {
	var result Container

	if err := p.call(ctx, p.client, http.MethodGet, "/api/containers/"+url.PathEscape(nameOrID), nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Version возвращает версию docker-controller.
func (p *Provider) Version(ctx context.Context) (string, error) {
	var result struct {
		Version string `json:"version"`
	}

	if err := p.call(ctx, p.client, http.MethodGet, "/api/version", nil, &result); err != nil {
		return "", err
	}

	return result.Version, nil
}

// PullImage загружает образ.
func (p *Provider) PullImage(ctx context.Context, image string) error {
	body := map[string]string{
		"image": image,
	}

	return p.call(ctx, p.runClient, http.MethodPost, "/api/images/pull", body, nil)
}

// ReplaceContainer заменяет образ контейнера name, сохраняя старый контейнер под именем rollbackName.
func (p *Provider) ReplaceContainer(ctx context.Context, name, image, rollbackName string) (*ReplaceResult, error) {
	body := map[string]string{
		"image":         image,
		"rollback_name": rollbackName,
	}

	var result ReplaceResult

	if err := p.call(ctx, p.runClient, http.MethodPost, "/api/containers/"+url.PathEscape(name)+"/replace", body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RestoreContainer возвращает исходный контейнер после ReplaceContainer (без запуска).
func (p *Provider) RestoreContainer(ctx context.Context, name, oldID, rollbackName string) error {
	body := map[string]string{
		"old_id":        oldID,
		"rollback_name": rollbackName,
	}

	return p.call(ctx, p.client, http.MethodPost, "/api/containers/"+url.PathEscape(name)+"/restore", body, nil)
}

// StartContainer запускает контейнер.
func (p *Provider) StartContainer(ctx context.Context, name string) error {
	return p.call(ctx, p.client, http.MethodPost, "/api/containers/"+url.PathEscape(name)+"/start", struct{}{}, nil)
}

// CommitContainer удаляет контейнер rollbackName, сохраненный для отката.
func (p *Provider) CommitContainer(ctx context.Context, name, rollbackName string) error {
	body := map[string]string{
		"rollback_name": rollbackName,
	}

	return p.call(ctx, p.client, http.MethodPost, "/api/containers/"+url.PathEscape(name)+"/commit", body, nil)
}

// SelfUpdate запускает обновление docker-controller до образа image. Возвращает job-контейнер.
func (p *Provider) SelfUpdate(ctx context.Context, image string) (*Container, error) {
	body := map[string]string{
		"image": image,
	}

	var result Container

	if err := p.call(ctx, p.runClient, http.MethodPost, "/api/self-update", body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// call выполняет запрос к API управления контейнерами.
func (p *Provider) call(ctx context.Context, client *http.Client, method, path string, body, result any) error {
	var reader io.Reader

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cannot marshal request: %w", err)
		}

		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseUrl+path, reader)
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("docker-controller is unavailable: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("cannot read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}

		message := strings.TrimSpace(string(raw))

		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error != "" {
			message = apiErr.Error
		}

		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrNotFound, message)
		}

		return fmt.Errorf("docker-controller returned %d: %s", resp.StatusCode, message)
	}

	if result == nil {
		return nil
	}

	if err = json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("cannot decode response: %w", err)
	}

	return nil
}
