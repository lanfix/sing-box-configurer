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
	"github.com/lanfix/sing-box-configurer/internal/settings"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

const (
	// Как долго кэшировать результат проверки обновлений.
	checkCacheTTL = time.Hour

	// Как долго кэшировать неудачную проверку: ошибка часто временная (например, DNS недоступен, пока
	// перезапускается sing-box), и держать ее в интерфейсе час нельзя.
	checkErrorCacheTTL = time.Minute

	// Сколько последних релизов показывать со списком изменений.
	maxChangelogReleases = 10

	// Через сколько повторить автоматическую проверку после ошибки, если интервал проверки больше.
	autoCheckRetry = 10 * time.Minute

	// Сколько ждать ответа при автоматической проверке.
	autoCheckTimeout = 2 * time.Minute
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

	// AutoCheck — включена автоматическая проверка, NextCheckAt — когда она выполнится.
	AutoCheck   bool       `json:"auto_check"`
	NextCheckAt *time.Time `json:"next_check_at,omitempty"`
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

	// autoCheck возвращает настройки автоматической проверки, nil — проверка не запущена.
	autoCheck func() settings.Updates
}

// NewService создает сервис обновлений для платформы установки.
func NewService(p platform.Platform) *Service {
	return &Service{
		platform:  p.Name,
		updates:   p.Updates,
		mu:        sync.Mutex{},
		lastCheck: nil,
		autoCheck: nil,
	}
}

// Check возвращает доступные обновления. Проверка без force берет результат из кэша (успешный — на час
// или на интервал автоматической проверки, если он больше; ошибку — на минуту), force всегда проверяет заново.
func (s *Service) Check(ctx context.Context, force bool) *CheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if force || s.lastCheck == nil || time.Since(s.lastCheck.CheckedAt) >= s.cacheTTLLocked(s.lastCheck) {
		s.fetchLocked(ctx)
	}

	return s.withScheduleLocked(s.lastCheck)
}

// StartAutoCheck запускает автоматическую проверку обновлений с настройками из getter. Настройки
// перечитываются каждую минуту, поэтому изменения действуют сразу.
func (s *Service) StartAutoCheck(ctx context.Context, getter func() settings.Updates) {
	s.mu.Lock()
	s.autoCheck = getter
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		// Версия, о которой уже написано в журнале.
		var notified string

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
			}

			result := s.autoCheckTick(ctx)
			if result == nil {
				continue
			}

			if result.Error != "" {
				log.Printf("Automatic update check failed: %s", result.Error)

				continue
			}

			if len(result.Available) > 0 && result.LatestVersion != notified {
				notified = result.LatestVersion
				log.Printf("Update available: %s (current %s)", result.LatestVersion, result.CurrentVersion)
			}
		}
	}()
}

// autoCheckTick проверяет обновления, если автоматическая проверка включена и подошло ее время. Возвращает
// результат проверки или nil, если проверки не было.
func (s *Service) autoCheckTick(ctx context.Context) *CheckResult {
	s.mu.Lock()
	next, enabled := s.nextCheckLocked()
	s.mu.Unlock()

	if !enabled || time.Now().Before(next) {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, autoCheckTimeout)
	defer cancel()

	return s.Check(ctx, true)
}

// nextCheckLocked возвращает время следующей автоматической проверки и признак, что она включена.
// Вызывается под s.mu.
func (s *Service) nextCheckLocked() (time.Time, bool) {
	if s.autoCheck == nil {
		return time.Time{}, false
	}

	config := s.autoCheck()

	if !config.AutoCheck {
		return time.Time{}, false
	}

	// Проверок еще не было — проверяем сразу.
	if s.lastCheck == nil {
		return time.Now(), true
	}

	interval := config.Interval()

	if s.lastCheck.Error != "" {
		interval = min(interval, autoCheckRetry)
	}

	return s.lastCheck.CheckedAt.Add(interval), true
}

// cacheTTLLocked возвращает, сколько хранить результат проверки: неудачную — минуту, успешную — час или
// интервал автоматической проверки, если он больше. Вызывается под s.mu.
func (s *Service) cacheTTLLocked(result *CheckResult) time.Duration {
	if result.Error != "" {
		return checkErrorCacheTTL
	}

	if s.autoCheck != nil {
		if config := s.autoCheck(); config.AutoCheck {
			return max(checkCacheTTL, config.Interval())
		}
	}

	return checkCacheTTL
}

// withScheduleLocked возвращает копию результата с состоянием автоматической проверки. Вызывается под s.mu.
func (s *Service) withScheduleLocked(result *CheckResult) *CheckResult {
	copied := *result

	next, enabled := s.nextCheckLocked()

	copied.AutoCheck = enabled
	copied.NextCheckAt = nil

	if enabled {
		copied.NextCheckAt = &next
	}

	return &copied
}

// fetchLocked запрашивает релизы у платформы и сохраняет результат в кэш. Вызывается под s.mu.
func (s *Service) fetchLocked(ctx context.Context) {
	result := &CheckResult{
		CurrentVersion: version.Version,
		LatestVersion:  "",
		Available:      []platform.Release{},
		CheckedAt:      time.Now(),
		Error:          "",
		Platform:       s.platform,
		Unsupported:    s.updates.Unsupported(ctx),
		AutoCheck:      false,
		NextCheckAt:    nil,
	}

	s.lastCheck = result

	releases, err := s.updates.Releases(ctx)
	if err != nil {
		result.Error = err.Error()

		return
	}

	// Обновление через интерфейс недоступно: показываем только последний релиз для справки.
	if result.Unsupported != "" {
		for _, release := range releases {
			if result.LatestVersion == "" || semver.Compare(release.Version, result.LatestVersion) > 0 {
				result.LatestVersion = release.Version
			}
		}

		return
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
