package dockercontroller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type Provider struct {
	baseUrl string
	client  *http.Client
}

func NewProvider(baseUrl string) *Provider {
	if strings.HasSuffix(baseUrl, "/") {
		baseUrl = baseUrl[:len(baseUrl)-1]
	}

	client := &http.Client{
		Timeout: time.Second * 30,
	}

	return &Provider{
		baseUrl: baseUrl,
		client:  client,
	}
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
