package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/semver"
)

// Лейбл образа со списком изменений релиза (заполняется release.sh из CHANGELOG.md).
const changelogLabel = "io.lanfix.changelog"

// Release — опубликованная версия образа.
type Release struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Changelog   string    `json:"changelog"`
}

// Registry читает теги и лейблы образов из Docker Hub.
type Registry struct {
	httpClient *http.Client
}

// NewRegistry создает клиент Docker Hub.
func NewRegistry() *Registry {
	return &Registry{
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Releases возвращает релизные версии (semver без pre-release) репозитория вида lanfix/sing-box-configurer.
func (r *Registry) Releases(ctx context.Context, repository string) ([]Release, error) {
	next := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", repository)
	result := make([]Release, 0)

	// Ограничиваем число страниц, чтобы не зациклиться на некорректном ответе.
	for page := 0; next != "" && page < 10; page++ {
		var response struct {
			Next    string `json:"next"`
			Results []struct {
				Name        string    `json:"name"`
				LastUpdated time.Time `json:"last_updated"`
			} `json:"results"`
		}

		if err := r.getJSON(ctx, next, "", &response); err != nil {
			return nil, fmt.Errorf("cannot list tags: %w", err)
		}

		for _, tag := range response.Results {
			parsed, err := semver.Parse(tag.Name)
			if err != nil || parsed.Prerelease != "" {
				continue
			}

			result = append(result, Release{
				Version:     tag.Name,
				PublishedAt: tag.LastUpdated,
				Changelog:   "",
			})
		}

		next = response.Next
	}

	return result, nil
}

// Changelog возвращает значение лейбла io.lanfix.changelog образа repository:tag,
// читая только манифест и конфиг образа через Registry API (без скачивания слоев).
func (r *Registry) Changelog(ctx context.Context, repository, tag string) (string, error) {
	token, err := r.token(ctx, repository)
	if err != nil {
		return "", err
	}

	base := "https://registry-1.docker.io/v2/" + repository

	manifest, err := r.manifest(ctx, base+"/manifests/"+tag, token)
	if err != nil {
		return "", err
	}

	// Для multi-arch образа выбираем linux/amd64 (платформа сервера).
	if len(manifest.Manifests) > 0 {
		digest := manifest.Manifests[0].Digest

		for _, item := range manifest.Manifests {
			if item.Platform.OS == "linux" && item.Platform.Architecture == "amd64" {
				digest = item.Digest

				break
			}
		}

		if manifest, err = r.manifest(ctx, base+"/manifests/"+digest, token); err != nil {
			return "", err
		}
	}

	if manifest.Config.Digest == "" {
		return "", fmt.Errorf("manifest has no config")
	}

	var config struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}

	if err = r.getJSON(ctx, base+"/blobs/"+manifest.Config.Digest, token, &config); err != nil {
		return "", fmt.Errorf("cannot get image config: %w", err)
	}

	return strings.TrimSpace(config.Config.Labels[changelogLabel]), nil
}

// imageManifest — нужные поля манифеста образа или индекса.
type imageManifest struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
}

// manifest загружает манифест.
func (r *Registry) manifest(ctx context.Context, url, token string) (imageManifest, error) {
	var manifest imageManifest

	if err := r.getJSON(ctx, url, token, &manifest); err != nil {
		return imageManifest{}, fmt.Errorf("cannot get manifest: %w", err)
	}

	return manifest, nil
}

// token получает анонимный токен на чтение репозитория.
func (r *Registry) token(ctx context.Context, repository string) (string, error) {
	query := url.Values{}
	query.Set("service", "registry.docker.io")
	query.Set("scope", "repository:"+repository+":pull")

	var response struct {
		Token string `json:"token"`
	}

	if err := r.getJSON(ctx, "https://auth.docker.io/token?"+query.Encode(), "", &response); err != nil {
		return "", fmt.Errorf("cannot get registry token: %w", err)
	}

	return response.Token, nil
}

// getJSON выполняет GET-запрос и декодирует JSON-ответ.
func (r *Registry) getJSON(ctx context.Context, url, token string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", strings.Join([]string{
			"application/vnd.oci.image.index.v1+json",
			"application/vnd.oci.image.manifest.v1+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.docker.distribution.manifest.v2+json",
		}, ", "))
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("%s returned %d: %s", req.URL.Host, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}
