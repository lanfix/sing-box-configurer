package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/dockerapi"
	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/updater"
)

const (
	// LocalDeployDir — точка монтирования папки деплоя в контейнере updater.
	LocalDeployDir = "/deploy"

	// imageVersionLabel — лейбл образа с версией конфигуратора.
	imageVersionLabel = "org.opencontainers.image.version"
)

// Ключи состояния в журнале обновления.
const (
	stateName         = "container"
	stateOldID        = "old_id"
	stateOldImage     = "old_image"
	stateNewImage     = "new_image"
	stateNewID        = "new_id"
	stateRollbackName = "rollback_name"
	stateHostDir      = "host_dir"
)

// Target заменяет контейнер конфигуратора контейнером из образа новой версии. Прежний контейнер
// останавливается и переименовывается (не удаляется), новый создается с теми же томами, портами,
// сетями и лейблами. Работает внутри job-контейнера updater.
type Target struct {
	docker    *dockerapi.Manager
	container string
	log       *slog.Logger
}

// NewTarget создает Target для контейнера конфигуратора container.
func NewTarget(ctx context.Context, container string, logger *slog.Logger) (*Target, error) {
	if container == "" {
		return nil, fmt.Errorf("configurer container is required")
	}

	manager, err := dockerapi.NewManager()
	if err != nil {
		return nil, err
	}

	if err = manager.Ping(ctx); err != nil {
		return nil, err
	}

	return &Target{
		docker:    manager,
		container: container,
		log:       logger,
	}, nil
}

// UpdatesDir возвращает папку журналов обновлений в папке деплоя.
func UpdatesDir() string {
	return LocalDeployDir + "/" + updater.UpdatesDirName
}

// Root возвращает папку деплоя: файлы для бэкапа заданы относительно нее.
func (t *Target) Root() string {
	return LocalDeployDir
}

// Prepare определяет текущую версию по тегу образа и возвращает смонтированные в конфигуратор
// файлы и каталоги из папки деплоя, а также compose-файлы.
func (t *Target) Prepare(ctx context.Context, journal *updater.Journal) ([]string, error) {
	info, err := t.docker.Inspect(ctx, t.container)
	if err != nil {
		return nil, err
	}

	repository, current := splitImage(info.Image)

	// Тег latest не говорит о версии — ее знает лейбл образа (наследуется контейнером).
	if !semver.IsValid(current) {
		current = info.Labels[imageVersionLabel]
	}

	journal.FromVersion = current

	workingDir := info.Labels[composeWorkingDirLabel]
	if workingDir == "" {
		return nil, fmt.Errorf("container %s is not managed by docker compose (no %s label)", info.Name, composeWorkingDirLabel)
	}

	if _, err = os.Stat(LocalDeployDir); err != nil {
		return nil, fmt.Errorf("deploy dir is not mounted to %s: %w", LocalDeployDir, err)
	}

	journal.State[stateName] = info.Name
	journal.State[stateOldID] = info.ID
	journal.State[stateOldImage] = info.Image
	journal.State[stateNewImage] = repository + ":" + journal.ToVersion
	journal.State[stateRollbackName] = fmt.Sprintf("%s-rollback-%s", info.Name, journal.ID)
	journal.State[stateHostDir] = workingDir

	paths := deployPaths{
		hostDir: workingDir,
	}

	hostPaths := make([]string, 0, len(info.Mounts))

	for _, mount := range info.Mounts {
		if mount.Type == "bind" {
			hostPaths = append(hostPaths, mount.Source)
		}
	}

	hostPaths = append(hostPaths, composeFiles(info.Labels)...)

	files := make([]string, 0, len(hostPaths))

	// Пути вне папки деплоя (например, docker.sock) не бэкапятся.
	for _, hostPath := range hostPaths {
		if rel, ok := paths.relative(hostPath); ok {
			files = append(files, rel)
		}
	}

	return files, nil
}

// Download загружает образ новой версии.
func (t *Target) Download(ctx context.Context, journal *updater.Journal) error {
	image := journal.State[stateNewImage]

	t.log.Info("pulling image "+image, "step", updater.StepDownload)

	return t.docker.Pull(ctx, image)
}

// Replace заменяет контейнер конфигуратора контейнером из нового образа.
func (t *Target) Replace(ctx context.Context, journal *updater.Journal) error {
	result, err := t.docker.Replace(ctx, journal.State[stateName], journal.State[stateNewImage], journal.State[stateRollbackName])

	// ID нового контейнера сохраняется и при ошибке запуска: по нему откат удалит контейнер.
	journal.State[stateNewID] = result.NewID

	if err != nil {
		return err
	}

	t.log.Info("container recreated", "step", updater.StepReplace, "container", journal.State[stateName], "image", journal.State[stateNewImage])

	return nil
}

// Alive проверяет, что новый контейнер не завершился.
func (t *Target) Alive(ctx context.Context, journal *updater.Journal) error {
	info, err := t.docker.Inspect(ctx, journal.State[stateNewID])
	if err != nil {
		return err
	}

	if info.Exited() {
		return fmt.Errorf("container exited with code %d", info.ExitCode)
	}

	return nil
}

// Logs возвращает последние строки логов нового контейнера.
func (t *Target) Logs(ctx context.Context, journal *updater.Journal) string {
	logs, err := t.docker.Logs(ctx, journal.State[stateNewID], "40")
	if err != nil {
		return ""
	}

	return logs
}

// Finish прописывает новый тег образа в compose-файлы, чтобы docker compose up не откатил версию.
func (t *Target) Finish(ctx context.Context, journal *updater.Journal) error {
	info, err := t.docker.Inspect(ctx, journal.State[stateName])
	if err != nil {
		return err
	}

	paths := deployPaths{
		hostDir: journal.State[stateHostDir],
	}

	repository, tag := splitImage(journal.State[stateNewImage])

	for _, file := range composeFiles(info.Labels) {
		rel, ok := paths.relative(file)
		if !ok {
			t.log.Warn("compose file is outside deploy dir, skipping", "step", updater.StepFinish, "file", file)

			continue
		}

		changed, err := setComposeImageTag(LocalDeployDir+"/"+rel, repository, tag)
		if err != nil {
			return err
		}

		if changed {
			t.log.Info("compose file updated", "step", updater.StepFinish, "file", rel, "image", journal.State[stateNewImage])
		}
	}

	return nil
}

// Restore удаляет новый контейнер и возвращает имя прежнему.
func (t *Target) Restore(ctx context.Context, journal *updater.Journal) error {
	return t.docker.Restore(ctx, journal.State[stateName], journal.State[stateOldID], journal.State[stateRollbackName])
}

// StartPrevious запускает прежний контейнер.
func (t *Target) StartPrevious(ctx context.Context, journal *updater.Journal) error {
	return t.docker.Start(ctx, journal.State[stateName])
}

// Commit удаляет прежний контейнер, сохраненный для отката.
func (t *Target) Commit(ctx context.Context, journal *updater.Journal) error {
	name, rollbackName := journal.State[stateName], journal.State[stateRollbackName]

	if err := dockerapi.ValidateRollbackName(name, rollbackName); err != nil {
		return err
	}

	return t.docker.Remove(ctx, rollbackName)
}

// splitImage разделяет образ на репозиторий и тег.
func splitImage(ref string) (string, string) {
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")

	if colon > slash {
		return ref[:colon], ref[colon+1:]
	}

	return ref, "latest"
}
