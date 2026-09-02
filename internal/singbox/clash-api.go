package singbox

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ClashAPI struct {
	baseUrl string
	client  *http.Client
}

func NewClashAPI(baseUrl string) *ClashAPI {
	if strings.HasSuffix(baseUrl, "/") {
		baseUrl = baseUrl[:len(baseUrl)-1]
	}

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	return &ClashAPI{
		baseUrl: baseUrl,
		client:  client,
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
