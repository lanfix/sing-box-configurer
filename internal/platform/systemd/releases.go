package systemd

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/semver"
)

const (
	// checksumsName — файл с SHA-256 архивов релиза.
	checksumsName = "SHA256SUMS"

	// maxArchiveSize ограничивает размер загружаемого архива релиза.
	maxArchiveSize = 256 << 20
)

// releaseBinaries — файлы архива релиза, которые нужны для обновления.
var releaseBinaries = []string{"sing-box-configurer", "updater"}

// releases читает релизы конфигуратора из GitHub Releases. Список изменений — текст релиза.
type releases struct {
	repository string
	http       *http.Client

	// Тексты релизов из последнего списка: без кэша проверка обновлений тратила бы лимит API GitHub
	// (60 запросов в час без токена) на каждую версию.
	mu    sync.Mutex
	notes map[string]string
}

// newReleases создает клиент GitHub Releases репозитория вида lanfix/sing-box-configurer.
func newReleases(repository string) *releases {
	return &releases{
		repository: repository,
		http: &http.Client{
			Timeout: 10 * time.Minute,
		},
		mu:    sync.Mutex{},
		notes: map[string]string{},
	}
}

// githubRelease — нужные поля релиза GitHub.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

// List возвращает опубликованные релизные версии (semver без pre-release).
func (r *releases) List(ctx context.Context) ([]platform.Release, error) {
	var items []githubRelease

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100", r.repository)

	if err := r.getJSON(ctx, url, &items); err != nil {
		return nil, fmt.Errorf("cannot list releases: %w", err)
	}

	result := make([]platform.Release, 0, len(items))
	notes := make(map[string]string, len(items))

	for _, item := range items {
		parsed, err := semver.Parse(item.TagName)
		if err != nil || parsed.Prerelease != "" || item.Draft || item.Prerelease {
			continue
		}

		notes[item.TagName] = strings.TrimSpace(item.Body)

		result = append(result, platform.Release{
			Version:     item.TagName,
			PublishedAt: item.PublishedAt,
			Changelog:   "",
		})
	}

	r.mu.Lock()
	r.notes = notes
	r.mu.Unlock()

	return result, nil
}

// Notes возвращает текст релиза version.
func (r *releases) Notes(ctx context.Context, version string) (string, error) {
	r.mu.Lock()
	notes, ok := r.notes[version]
	r.mu.Unlock()

	if ok {
		return notes, nil
	}

	var item githubRelease

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", r.repository, version)

	if err := r.getJSON(ctx, url, &item); err != nil {
		return "", fmt.Errorf("cannot get release %s: %w", version, err)
	}

	return strings.TrimSpace(item.Body), nil
}

// Download загружает архив релиза version для текущей архитектуры, проверяет его SHA-256
// и распаковывает бинарники в dir.
func (r *releases) Download(ctx context.Context, version, dir string) error {
	arch, err := releaseArch()
	if err != nil {
		return err
	}

	archive, err := r.downloadVerified(ctx, version, fmt.Sprintf("sing-box-configurer_%s_linux_%s.tar.gz", version, arch))
	if err != nil {
		return err
	}

	return extractBinaries(archive, dir, releaseBinaries)
}

// downloadVerified загружает файл name релиза version и сверяет его SHA-256 с SHA256SUMS релиза.
func (r *releases) downloadVerified(ctx context.Context, version, name string) ([]byte, error) {
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s/", r.repository, version)

	checksums, err := r.download(ctx, base+checksumsName, 1<<20)
	if err != nil {
		return nil, err
	}

	want, err := findChecksum(checksums, name)
	if err != nil {
		return nil, err
	}

	archive, err := r.download(ctx, base+name, maxArchiveSize)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(archive)

	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, want)
	}

	return archive, nil
}

// download загружает файл размером не больше limit байт.
func (r *releases) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := r.get(ctx, url)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot download %s: %w", url, err)
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, limit)
	}

	return data, nil
}

// getJSON выполняет запрос к API GitHub и декодирует JSON-ответ.
func (r *releases) getJSON(ctx context.Context, url string, v any) error {
	resp, err := r.get(ctx, url)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

// get выполняет GET-запрос. Ответ не 200 — ошибка.
func (r *releases) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sing-box-configurer")

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()

		return nil, fmt.Errorf("%s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return resp, nil
}

// releaseArch возвращает архитектуру в имени архива релиза.
func releaseArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH, nil

	case "arm":
		return "armv7", nil
	}

	return "", fmt.Errorf("architecture %s is not supported by releases", runtime.GOARCH)
}

// findChecksum ищет SHA-256 файла name в выводе sha256sum.
func findChecksum(checksums []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}

	return "", fmt.Errorf("no checksum for %s in %s", name, checksumsName)
}

// extractBinaries распаковывает из tar.gz файлы с именами names (в любом каталоге архива) в dir.
func extractBinaries(archive []byte, dir string, names []string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("cannot open archive: %w", err)
	}

	defer func() {
		_ = gz.Close()
	}()

	if err = os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	reader := tar.NewReader(gz)
	found := map[string]bool{}

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("cannot read archive: %w", err)
		}

		name := path.Base(header.Name)

		if header.Typeflag != tar.TypeReg || !slices.Contains(names, name) {
			continue
		}

		if err = writeExecutable(filepath.Join(dir, name), io.LimitReader(reader, maxArchiveSize)); err != nil {
			return err
		}

		found[name] = true
	}

	for _, name := range names {
		if !found[name] {
			return fmt.Errorf("archive has no %s", name)
		}
	}

	return nil
}

// writeExecutable записывает исполняемый файл.
func writeExecutable(target string, source io.Reader) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", target, err)
	}

	if _, err = io.Copy(file, source); err != nil {
		_ = file.Close()

		return fmt.Errorf("cannot write %s: %w", target, err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", target, err)
	}

	return nil
}
