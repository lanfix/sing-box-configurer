package docker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/lanfix/sing-box-configurer/internal/dockerapi"
	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

const (
	// UpdaterContainerName — фиксированное имя job-контейнера: Docker не даст запустить два обновления сразу.
	UpdaterContainerName = "sing-box-configurer-updater"

	// updaterLabel помечает job-контейнеры обновления.
	updaterLabel = "io.lanfix.updater"

	// Лейблы docker compose.
	composeWorkingDirLabel  = "com.docker.compose.project.working_dir"
	composeConfigFilesLabel = "com.docker.compose.project.config_files"

	// dockerSocket — путь сокета Docker в контейнерах конфигуратора и updater.
	dockerSocket = "/var/run/docker.sock"

	// Репозиторий образа конфигуратора в Docker Hub.
	defaultRepository = "lanfix/sing-box-configurer"
)

// updates обновляет конфигуратор заменой контейнера: updater запускается job-контейнером из образа
// новой версии с доступом к Docker API и папке деплоя (working_dir проекта docker compose).
type updates struct {
	docker     *dockerapi.Manager
	registry   *Registry
	listenPort string
}

// Releases возвращает теги образа конфигуратора в Docker Hub.
func (u *updates) Releases(ctx context.Context) ([]platform.Release, error) {
	repository, _ := u.repository(ctx)

	return u.registry.Releases(ctx, repository)
}

// Changelog возвращает список изменений из лейбла образа версии.
func (u *updates) Changelog(ctx context.Context, version string) (string, error) {
	repository, _ := u.repository(ctx)

	return u.registry.Changelog(ctx, repository, version)
}

// Unsupported возвращает причину, по которой обновление через интерфейс недоступно.
func (u *updates) Unsupported(ctx context.Context) string {
	if err := u.docker.Ping(ctx); err != nil {
		return fmt.Sprintf("Нет доступа к Docker API: смонтируйте %s в контейнер конфигуратора (%v).", dockerSocket, err)
	}

	if _, localImage := u.repository(ctx); localImage != "" {
		return fmt.Sprintf("Конфигуратор запущен из локально собранного образа %s. Чтобы обновиться, пересоберите его: docker compose up -d --build.", localImage)
	}

	if !semver.IsValid(version.Version) {
		return fmt.Sprintf("Сборка %s не является релизом: обновление через интерфейс недоступно, пересоберите образ из новой версии кода.", version.Version)
	}

	return ""
}

// Start запускает job-контейнер updater из образа версии target.
func (u *updates) Start(ctx context.Context, updateID, target string) (time.Time, error) {
	self, err := u.self(ctx)
	if err != nil {
		return time.Time{}, err
	}

	if _, local := dockerHubRepository(self.Image); local {
		return time.Time{}, errors.New(u.Unsupported(ctx))
	}

	workingDir := self.Labels[composeWorkingDirLabel]
	if workingDir == "" {
		return time.Time{}, fmt.Errorf("container %s is not managed by docker compose", self.Name)
	}

	if len(self.Networks) == 0 {
		return time.Time{}, fmt.Errorf("container %s has no networks", self.Name)
	}

	socket := mountSource(self, dockerSocket)
	if socket == "" {
		return time.Time{}, fmt.Errorf("%s is not mounted into container %s", dockerSocket, self.Name)
	}

	repository, _, _ := strings.Cut(self.Image, ":")
	if strings.Count(self.Image, ":") > 1 || strings.Contains(repository, "@") {
		return time.Time{}, fmt.Errorf("unsupported image reference %q", self.Image)
	}

	if err = u.removeFinishedUpdater(ctx); err != nil {
		return time.Time{}, err
	}

	job, err := u.docker.Run(ctx, dockerapi.RunRequest{
		Image: repository + ":" + target,
		Name:  UpdaterContainerName,
		Cmd:   []string{"/app/updater"},
		Env: []string{
			"UPDATE_PLATFORM=" + platform.NameDocker,
			"UPDATE_ID=" + updateID,
			"CONFIGURER_CONTAINER=" + self.Name,
			fmt.Sprintf("CONFIGURER_HEALTH_URL=http://%s:%s/api/health", self.Name, u.listenPort),
		},
		Labels: map[string]string{
			updaterLabel:             "true",
			updaterLabel + ".id":     updateID,
			updaterLabel + ".target": target,
		},
		Binds: []string{
			workingDir + ":" + LocalDeployDir,
			socket + ":" + dockerSocket,
		},
		Network: self.Networks[0],
		RestartPolicy: container.RestartPolicy{
			Name:              container.RestartPolicyOnFailure,
			MaximumRetryCount: 3,
		},
		Pull: true,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot start updater: %w", err)
	}

	log.Printf("Update %s to %s started in container %s", updateID, target, job.Name)

	startedAt, _ := time.Parse(time.RFC3339Nano, job.StartedAt)

	return startedAt, nil
}

// Job возвращает job-контейнер последнего обновления и его логи.
func (u *updates) Job(ctx context.Context) (platform.Job, error) {
	containers, err := u.updaterContainers(ctx)
	if err != nil {
		return platform.Job{}, err
	}

	if len(containers) == 0 {
		return platform.Job{}, nil
	}

	job := containers[0]

	logs, err := u.docker.Logs(ctx, job.ID, "all")
	if err != nil {
		return platform.Job{}, err
	}

	return platform.Job{
		Exists:     true,
		Running:    job.Running || job.Restarting,
		UpdateID:   job.Labels[updaterLabel+".id"],
		Target:     job.Labels[updaterLabel+".target"],
		StartedAt:  job.StartedAt,
		FinishedAt: job.FinishedAt,
		ExitCode:   job.ExitCode,
		Logs:       logs,
	}, nil
}

// updaterContainers возвращает job-контейнеры обновлений.
func (u *updates) updaterContainers(ctx context.Context) ([]dockerapi.Info, error) {
	return u.docker.List(ctx, dockerapi.Filter{
		ID:   "",
		Name: "",
		Labels: map[string]string{
			updaterLabel: "true",
		},
	})
}

// self находит контейнер конфигуратора.
func (u *updates) self(ctx context.Context) (dockerapi.Info, error) {
	return selfContainer(ctx, u.docker)
}

// selfContainer находит контейнер конфигуратора. Hostname контейнера по умолчанию — его короткий ID.
func selfContainer(ctx context.Context, docker *dockerapi.Manager) (dockerapi.Info, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return dockerapi.Info{}, fmt.Errorf("cannot get hostname: %w", err)
	}

	containers, err := docker.List(ctx, dockerapi.Filter{
		ID:     hostname,
		Name:   "",
		Labels: nil,
	})
	if err != nil {
		return dockerapi.Info{}, err
	}

	if len(containers) != 1 {
		return dockerapi.Info{}, fmt.Errorf("cannot find own container by hostname %q (is the service running in docker without custom hostname?)", hostname)
	}

	return containers[0], nil
}

// removeFinishedUpdater удаляет завершенный job-контейнер прошлого обновления.
func (u *updates) removeFinishedUpdater(ctx context.Context) error {
	containers, err := u.updaterContainers(ctx)
	if err != nil {
		return err
	}

	for _, job := range containers {
		if job.Running || job.Restarting {
			return errors.New("update is already in progress")
		}

		if err = u.docker.Remove(ctx, job.ID); err != nil {
			return fmt.Errorf("cannot remove previous updater: %w", err)
		}
	}

	return nil
}

// repository возвращает репозиторий образа в Docker Hub (например lanfix/sing-box-configurer) и имя образа,
// если он собран локально (docker compose build) — тогда используется репозиторий по умолчанию.
// Если свой контейнер найти нельзя (нет доступа к Docker API), тоже используется репозиторий по умолчанию.
func (u *updates) repository(ctx context.Context) (string, string) {
	self, err := u.self(ctx)
	if err != nil {
		log.Printf("Cannot detect own image, using default repository: %v", err)

		return defaultRepository, ""
	}

	if repository, local := dockerHubRepository(self.Image); !local {
		return repository, ""
	}

	return defaultRepository, self.Image
}

// dockerHubRepository возвращает репозиторий образа в Docker Hub. local — образ собран локально
// (например, deploy-sing-box-configurer после docker compose build) и в Docker Hub его нет.
func dockerHubRepository(image string) (string, bool) {
	repository, _, _ := strings.Cut(image, ":")
	repository = strings.TrimPrefix(repository, "docker.io/")

	return repository, strings.Count(repository, "/") != 1
}

// mountSource возвращает путь на хосте, смонтированный в контейнер по пути destination.
func mountSource(info dockerapi.Info, destination string) string {
	for _, mount := range info.Mounts {
		if mount.Destination == destination {
			return mount.Source
		}
	}

	return ""
}
