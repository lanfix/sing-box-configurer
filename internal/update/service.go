// Package update проверяет наличие новых версий и запускает обновление средствами платформы установки.
package update

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

const (
	// Как долго кэшировать результат проверки обновлений.
	checkCacheTTL = time.Hour

	// Сколько последних релизов показывать со списком изменений.
	maxChangelogReleases = 10
)

// CheckResult — результат проверки обновлений.
type CheckResult struct {
	CurrentVersion string             `json:"current_version"`
	LatestVersion  string             `json:"latest_version"`
	Available      []platform.Release `json:"available"`
	CheckedAt      time.Time          `json:"checked_at"`
	Error          string             `json:"error,omitempty"`

	// Platform — способ установки (docker, systemd).
	Platform string `json:"platform"`

	// Unsupported — почему обновление через интерфейс недоступно (например, локальная сборка). Это не ошибка:
	// версии для справки все равно проверяются, но Available остается пустым.
	Unsupported string `json:"unsupported,omitempty"`
}

// StatusStep — строка прогресса updater.
type StatusStep struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Step    string `json:"step"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// Status — состояние последнего обновления.
type Status struct {
	Exists      bool         `json:"exists"`
	Running     bool         `json:"running"`
	UpdateID    string       `json:"update_id"`
	Target      string       `json:"target"`
	Result      string       `json:"result"`
	FromVersion string       `json:"from_version"`
	ToVersion   string       `json:"to_version"`
	Error       string       `json:"error"`
	Steps       []StatusStep `json:"steps"`
	StartedAt   string       `json:"started_at"`
	FinishedAt  string       `json:"finished_at"`
}

// Service управляет проверкой и запуском обновлений.
type Service struct {
	platform string
	updates  platform.Updates

	mu        sync.Mutex
	lastCheck *CheckResult
}

// NewService создает сервис обновлений для платформы установки.
func NewService(p platform.Platform) *Service {
	return &Service{
		platform:  p.Name,
		updates:   p.Updates,
		mu:        sync.Mutex{},
		lastCheck: nil,
	}
}

// Check возвращает доступные обновления. Результат кэшируется на час, force сбрасывает кэш.
func (s *Service) Check(ctx context.Context, force bool) *CheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !force && s.lastCheck != nil && time.Since(s.lastCheck.CheckedAt) < checkCacheTTL {
		return s.lastCheck
	}

	result := &CheckResult{
		CurrentVersion: version.Version,
		LatestVersion:  "",
		Available:      []platform.Release{},
		CheckedAt:      time.Now(),
		Error:          "",
		Platform:       s.platform,
		Unsupported:    s.updates.Unsupported(ctx),
	}

	s.lastCheck = result

	releases, err := s.updates.Releases(ctx)
	if err != nil {
		result.Error = err.Error()

		return result
	}

	// Обновление через интерфейс недоступно: показываем только последний релиз для справки.
	if result.Unsupported != "" {
		for _, release := range releases {
			if result.LatestVersion == "" || semver.Compare(release.Version, result.LatestVersion) > 0 {
				result.LatestVersion = release.Version
			}
		}

		return result
	}

	for _, release := range releases {
		if semver.Compare(release.Version, version.Version) > 0 {
			result.Available = append(result.Available, release)
		}
	}

	// Новые версии первыми.
	slices.SortFunc(result.Available, func(a, b platform.Release) int {
		return semver.Compare(b.Version, a.Version)
	})

	for i := range result.Available {
		if i >= maxChangelogReleases {
			break
		}

		changelog, err := s.updates.Changelog(ctx, result.Available[i].Version)
		if err != nil {
			log.Printf("Cannot get changelog of %s: %v", result.Available[i].Version, err)

			continue
		}

		result.Available[i].Changelog = changelog
	}

	if len(result.Available) > 0 {
		result.LatestVersion = result.Available[0].Version
	}

	return result
}

// Start запускает обновление до версии target.
func (s *Service) Start(ctx context.Context, target string) (*Status, error) {
	if !semver.IsValid(version.Version) {
		return nil, fmt.Errorf("current build %q is not a release version, update is not supported", version.Version)
	}

	if !semver.IsValid(target) || semver.Compare(target, version.Version) <= 0 {
		return nil, fmt.Errorf("version %q is not newer than current %s", target, version.Version)
	}

	updateID := time.Now().UTC().Format("20060102-150405")

	startedAt, err := s.updates.Start(ctx, updateID, target)
	if err != nil {
		return nil, err
	}

	return &Status{
		Exists:      true,
		Running:     true,
		UpdateID:    updateID,
		Target:      target,
		Result:      "",
		FromVersion: version.Version,
		ToVersion:   target,
		Error:       "",
		Steps:       []StatusStep{},
		StartedAt:   startedAt.Format(time.RFC3339Nano),
		FinishedAt:  "",
	}, nil
}

// Status возвращает состояние последнего обновления по процессу updater и его логам.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	job, err := s.updates.Job(ctx)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Exists:      job.Exists,
		Running:     job.Running,
		UpdateID:    job.UpdateID,
		Target:      job.Target,
		Result:      "",
		FromVersion: "",
		ToVersion:   "",
		Error:       "",
		Steps:       []StatusStep{},
		StartedAt:   job.StartedAt,
		FinishedAt:  job.FinishedAt,
	}

	if !job.Exists {
		return status, nil
	}

	parseUpdaterLogs(job.Logs, status)

	// Updater завершился без итоговой строки — он упал и исчерпал перезапуски.
	if !status.Running && status.Result == "" {
		status.Result = "failed"
		status.Error = fmt.Sprintf("updater exited with code %d without result; last logs:\n%s", job.ExitCode, lastLines(job.Logs, 20))
	}

	return status, nil
}

// parseUpdaterLogs разбирает JSON-строки логов updater.
func parseUpdaterLogs(logs string, status *Status) {
	scanner := bufio.NewScanner(strings.NewReader(logs))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for scanner.Scan() {
		var line struct {
			Time        string `json:"time"`
			Level       string `json:"level"`
			Msg         string `json:"msg"`
			Step        string `json:"step"`
			Error       string `json:"error"`
			Result      string `json:"result"`
			FromVersion string `json:"from_version"`
			ToVersion   string `json:"to_version"`
		}

		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}

		if line.Result != "" {
			status.Result = line.Result
			status.Error = line.Error
			status.FromVersion = line.FromVersion
			status.ToVersion = line.ToVersion

			continue
		}

		status.Steps = append(status.Steps, StatusStep{
			Time:    line.Time,
			Level:   line.Level,
			Step:    line.Step,
			Message: line.Msg,
			Error:   line.Error,
		})
	}
}

// lastLines возвращает последние n строк текста.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
