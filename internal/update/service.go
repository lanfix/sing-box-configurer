// Package update проверяет наличие новых версий и запускает обновление через updater-job.
package update

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

const (
	// UpdaterContainerName — фиксированное имя job-контейнера: Docker не даст запустить два обновления сразу.
	UpdaterContainerName = "sing-box-configurer-updater"

	// updaterLabel помечает job-контейнеры обновления.
	updaterLabel = "io.lanfix.updater"

	// Лейблы docker compose.
	composeServiceLabel    = "com.docker.compose.service"
	composeProjectLabel    = "com.docker.compose.project"
	composeWorkingDirLabel = "com.docker.compose.project.working_dir"

	// Имя сервиса docker-controller в compose-файле.
	controllerServiceName = "docker-controller"

	// Как долго кэшировать результат проверки обновлений.
	checkCacheTTL = time.Hour

	// Сколько последних релизов показывать со списком изменений.
	maxChangelogReleases = 10

	// Репозиторий образа конфигуратора в Docker Hub.
	defaultRepository = "lanfix/sing-box-configurer"
)

// CheckResult — результат проверки обновлений.
type CheckResult struct {
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	Available      []Release `json:"available"`
	CheckedAt      time.Time `json:"checked_at"`
	Error          string    `json:"error,omitempty"`
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
	controller *dockercontroller.Provider
	registry   *Registry
	listenPort string

	mu        sync.Mutex
	lastCheck *CheckResult
}

// NewService создает сервис обновлений. listenAddr — адрес HTTP-сервера конфигуратора (нужен порт для health-check).
func NewService(controller *dockercontroller.Provider, registry *Registry, listenAddr string) *Service {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		port = "8080"
	}

	return &Service{
		controller: controller,
		registry:   registry,
		listenPort: port,
		mu:         sync.Mutex{},
		lastCheck:  nil,
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
		Available:      []Release{},
		CheckedAt:      time.Now(),
		Error:          "",
	}

	s.lastCheck = result

	repository, err := s.repository(ctx)
	if err != nil {
		result.Error = err.Error()

		return result
	}

	releases, err := s.registry.Releases(ctx, repository)
	if err != nil {
		result.Error = err.Error()

		return result
	}

	for _, release := range releases {
		if semver.Compare(release.Version, version.Version) > 0 {
			result.Available = append(result.Available, release)
		}
	}

	// Новые версии первыми.
	slices.SortFunc(result.Available, func(a, b Release) int {
		return semver.Compare(b.Version, a.Version)
	})

	for i := range result.Available {
		if i >= maxChangelogReleases {
			break
		}

		changelog, err := s.registry.Changelog(ctx, repository, result.Available[i].Version)
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

	self, err := s.self(ctx)
	if err != nil {
		return nil, err
	}

	workingDir := self.Labels[composeWorkingDirLabel]
	if workingDir == "" {
		return nil, fmt.Errorf("container %s is not managed by docker compose", self.Name)
	}

	if len(self.Networks) == 0 {
		return nil, fmt.Errorf("container %s has no networks", self.Name)
	}

	controllerName, err := s.controllerName(ctx, self.Labels[composeProjectLabel])
	if err != nil {
		return nil, err
	}

	if err = s.removeFinishedUpdater(ctx); err != nil {
		return nil, err
	}

	repository, _, _ := strings.Cut(self.Image, ":")
	if strings.Count(self.Image, ":") > 1 || strings.Contains(repository, "@") {
		return nil, fmt.Errorf("unsupported image reference %q", self.Image)
	}

	updateID := time.Now().UTC().Format("20060102-150405")

	job, err := s.controller.RunContainer(ctx, dockercontroller.RunRequest{
		Image: repository + ":" + target,
		Name:  UpdaterContainerName,
		Cmd:   []string{"/app/updater"},
		Env: []string{
			"UPDATE_ID=" + updateID,
			"CONFIGURER_CONTAINER=" + self.Name,
			"CONTROLLER_CONTAINER=" + controllerName,
			fmt.Sprintf("CONFIGURER_HEALTH_URL=http://%s:%s/api/health", self.Name, s.listenPort),
			"CONTROLLER_URL=" + s.controller.BaseURL(),
			"CONTROLLER_API_KEY=" + s.controller.APIKey(),
		},
		Labels: map[string]string{
			updaterLabel:             "true",
			updaterLabel + ".id":     updateID,
			updaterLabel + ".target": target,
		},
		// Updater работает с Docker только через API контроллера — ему нужна лишь папка деплоя.
		Binds: []string{
			workingDir + ":/deploy",
		},
		Network:       self.Networks[0],
		RestartPolicy: "on-failure",
		MaxRetries:    3,
		Pull:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot start updater: %w", err)
	}

	log.Printf("Update %s to %s started in container %s", updateID, target, job.Name)

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
		StartedAt:   job.StartedAt,
		FinishedAt:  "",
	}, nil
}

// Status возвращает состояние последнего обновления по job-контейнеру и его логам.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	containers, err := s.controller.ListContainers(ctx, dockercontroller.ContainerFilter{
		ID:   "",
		Name: "",
		Labels: map[string]string{
			updaterLabel: "true",
		},
	})
	if err != nil {
		return nil, err
	}

	status := &Status{
		Exists:      false,
		Running:     false,
		UpdateID:    "",
		Target:      "",
		Result:      "",
		FromVersion: "",
		ToVersion:   "",
		Error:       "",
		Steps:       []StatusStep{},
		StartedAt:   "",
		FinishedAt:  "",
	}

	if len(containers) == 0 {
		return status, nil
	}

	job := containers[0]

	status.Exists = true
	status.Running = job.Running || job.State == "restarting"
	status.UpdateID = job.Labels[updaterLabel+".id"]
	status.Target = job.Labels[updaterLabel+".target"]
	status.StartedAt = job.StartedAt
	status.FinishedAt = job.FinishedAt

	logs, err := s.controller.ContainerLogs(ctx, job.ID, "all")
	if err != nil {
		return nil, err
	}

	parseUpdaterLogs(logs, status)

	// Updater завершился без итоговой строки — он упал и исчерпал перезапуски.
	if !status.Running && status.Result == "" {
		status.Result = "failed"
		status.Error = fmt.Sprintf("updater exited with code %d without result; last logs:\n%s", job.ExitCode, lastLines(logs, 20))
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

// self находит контейнер конфигуратора. Hostname контейнера по умолчанию — его короткий ID.
func (s *Service) self(ctx context.Context) (*dockercontroller.Container, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("cannot get hostname: %w", err)
	}

	containers, err := s.controller.ListContainers(ctx, dockercontroller.ContainerFilter{
		ID:     hostname,
		Name:   "",
		Labels: nil,
	})
	if err != nil {
		return nil, err
	}

	if len(containers) != 1 {
		return nil, fmt.Errorf("cannot find own container by hostname %q (is the service running in docker without custom hostname?)", hostname)
	}

	return &containers[0], nil
}

// controllerName находит контейнер docker-controller в том же compose-проекте.
func (s *Service) controllerName(ctx context.Context, project string) (string, error) {
	containers, err := s.controller.ListContainers(ctx, dockercontroller.ContainerFilter{
		ID:   "",
		Name: "",
		Labels: map[string]string{
			composeProjectLabel: project,
			composeServiceLabel: controllerServiceName,
		},
	})
	if err != nil {
		return "", err
	}

	if len(containers) != 1 {
		return "", fmt.Errorf("cannot find %s container in compose project %q", controllerServiceName, project)
	}

	return containers[0].Name, nil
}

// removeFinishedUpdater удаляет завершенный job-контейнер прошлого обновления.
func (s *Service) removeFinishedUpdater(ctx context.Context) error {
	containers, err := s.controller.ListContainers(ctx, dockercontroller.ContainerFilter{
		ID:   "",
		Name: "",
		Labels: map[string]string{
			updaterLabel: "true",
		},
	})
	if err != nil {
		return err
	}

	for _, job := range containers {
		if job.Running || job.State == "restarting" {
			return errors.New("update is already in progress")
		}

		if err = s.controller.DeleteContainer(ctx, job.ID); err != nil {
			return fmt.Errorf("cannot remove previous updater: %w", err)
		}
	}

	return nil
}

// repository возвращает репозиторий образа в Docker Hub (например lanfix/sing-box-configurer).
// Если свой контейнер найти нельзя (docker-controller недоступен), используется репозиторий по умолчанию.
func (s *Service) repository(ctx context.Context) (string, error) {
	self, err := s.self(ctx)
	if err != nil {
		log.Printf("Cannot detect own image, using default repository: %v", err)

		return defaultRepository, nil
	}

	repository, _, _ := strings.Cut(self.Image, ":")
	repository = strings.TrimPrefix(repository, "docker.io/")

	if strings.Count(repository, "/") != 1 {
		return "", fmt.Errorf("image %q is not a Docker Hub image", self.Image)
	}

	return repository, nil
}

// lastLines возвращает последние n строк текста.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
