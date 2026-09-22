// Package updater обновляет sing-box-configurer и docker-controller.
//
// Updater запускается как одноразовый контейнер (job) из образа целевой версии конфигуратора,
// поэтому логика обновления всегда соответствует версии, на которую выполняется обновление.
// С Docker updater работает только через API docker-controller. Каждое действие записывается
// в журнал; при ошибке или после падения updater откатывает контейнер и восстанавливает файлы из бэкапа.
package updater

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

// Версии, которые требует эта версия конфигуратора.
const (
	// ControllerVersion — версия docker-controller, с которой работает эта версия конфигуратора.
	// При изменении API контроллера версию нужно поднять: updater обновит контроллер первым.
	// Новые версии контроллера должны оставаться совместимыми со старыми версиями конфигуратора:
	// при откате конфигуратора контроллер не откатывается.
	ControllerVersion = "v0.0.2"

	// MinUpgradeFrom — минимальная версия, с которой можно обновиться на эту. Пустая — без ограничений.
	MinUpgradeFrom = ""
)

// ComponentConfigurer — название компонента конфигуратора в журнале.
const ComponentConfigurer = "sing-box-configurer"

// Шаги обновления (пишутся в журнал и логи).
const (
	stepPrepare    = "prepare"
	stepPull       = "pull"
	stepController = "update-controller"
	stepBackup     = "backup"
	stepConfigurer = "update-configurer"
	stepCompose    = "update-compose"
	stepCommit     = "commit"
	stepRollback   = "rollback"
)

// Лейблы docker compose, по которым находится папка деплоя.
const (
	composeWorkingDirLabel  = "com.docker.compose.project.working_dir"
	composeConfigFilesLabel = "com.docker.compose.project.config_files"
)

const (
	// LocalDeployDir — точка монтирования папки деплоя в контейнере updater.
	LocalDeployDir = "/deploy"

	// UpdatesDirName — папка с журналами и бэкапами обновлений внутри папки деплоя.
	UpdatesDirName = ".updates"

	// Сколько папок обновлений хранить.
	keepUpdates = 5

	configurerHealthTimeout = 2 * time.Minute
	controllerUpdateTimeout = 3 * time.Minute
)

// Options — параметры запуска updater (передаются конфигуратором через переменные окружения).
type Options struct {
	UpdateID            string
	ConfigurerContainer string
	ControllerContainer string
	ConfigurerHealthURL string
	ControllerURL       string
	ControllerAPIKey    string
}

// Updater выполняет обновление.
type Updater struct {
	opts       Options
	controller *dockercontroller.Provider
	http       *http.Client
	log        *slog.Logger
	journal    *Journal
	paths      deployPaths
}

// New создает updater.
func New(opts Options, logger *slog.Logger) (*Updater, error) {
	if opts.UpdateID == "" || strings.ContainsAny(opts.UpdateID, `/\.`) {
		return nil, fmt.Errorf("invalid update id %q", opts.UpdateID)
	}

	if opts.ConfigurerContainer == "" || opts.ConfigurerHealthURL == "" || opts.ControllerURL == "" {
		return nil, fmt.Errorf("configurer container, health url and controller url are required")
	}

	return &Updater{
		opts:       opts,
		controller: dockercontroller.NewProvider(opts.ControllerURL, opts.ControllerAPIKey),
		http: &http.Client{
			Timeout: 3 * time.Second,
		},
		log:     logger,
		journal: nil,
		paths: deployPaths{
			hostDir:  "",
			localDir: LocalDeployDir,
		},
	}, nil
}

// updatesDir возвращает папку журналов обновлений.
func (u *Updater) updatesDir() string {
	return filepath.Join(LocalDeployDir, UpdatesDirName)
}

// Run выполняет обновление. Если журнал с таким ID уже есть (updater перезапустился после падения),
// незавершенное обновление откатывается, а завершенное — только повторно сообщает результат.
func (u *Updater) Run(ctx context.Context) {
	journal, err := loadJournal(u.updatesDir(), u.opts.UpdateID)
	if err != nil {
		u.result(PhaseFailed, err)

		return
	}

	if journal != nil {
		u.journal = journal

		switch {
		case journal.IsFinal():
			u.log.Info("update already finished", "step", journal.Step)
			u.result(journal.Phase, errorFromText(journal.Error))

		case journal.Step == stepCommit:
			// Новая версия уже прошла проверку — завершаем фиксацию.
			u.log.Warn("updater restarted during commit, finishing commit", "step", stepCommit)
			u.finish(u.commit(ctx))

		default:
			u.log.Warn("updater restarted after crash, rolling back", "step", journal.Step)
			u.rollbackAndReport(ctx, fmt.Errorf("updater was interrupted at step %q", journal.Step))
		}

		return
	}

	u.journal = newJournal(u.updatesDir(), u.opts.UpdateID)

	if err = u.update(ctx); err != nil {
		u.rollbackAndReport(ctx, err)

		return
	}

	u.finish(u.commit(ctx))
}

// update выполняет обновление до фиксации.
func (u *Updater) update(ctx context.Context) error {
	if err := u.prepare(ctx); err != nil {
		return err
	}

	configurer := u.component(ComponentConfigurer)

	u.setStep(stepPull, "pulling image "+configurer.NewImage)

	if err := u.controller.PullImage(ctx, configurer.NewImage); err != nil {
		return err
	}

	if err := u.updateController(ctx); err != nil {
		return fmt.Errorf("docker-controller update failed: %w", err)
	}

	u.setStep(stepBackup, "backing up data files")

	if err := u.backup(ctx, configurer); err != nil {
		return err
	}

	u.setStep(stepConfigurer, fmt.Sprintf("updating sing-box-configurer %s -> %s", u.journal.FromVersion, u.journal.ToVersion))

	if err := u.replace(ctx, configurer); err != nil {
		return fmt.Errorf("sing-box-configurer update failed: %w", err)
	}

	u.setStep(stepCompose, "updating image tag in compose file")

	return u.updateCompose(configurer.NewImage)
}

// prepare проверяет возможность обновления и планирует замену конфигуратора.
func (u *Updater) prepare(ctx context.Context) error {
	u.setStep(stepPrepare, "checking current state")

	info, err := u.controller.GetContainer(ctx, u.opts.ConfigurerContainer)
	if err != nil {
		return err
	}

	repository, currentVersion := splitImage(info.Image)
	target := version.Version

	u.journal.FromVersion = currentVersion
	u.journal.ToVersion = target

	if !semver.IsValid(target) {
		return fmt.Errorf("updater version %q is not a release version", target)
	}

	if semver.Compare(target, currentVersion) <= 0 {
		return fmt.Errorf("target version %s is not newer than current %s", target, currentVersion)
	}

	if MinUpgradeFrom != "" && semver.Compare(currentVersion, MinUpgradeFrom) < 0 {
		return fmt.Errorf("upgrade to %s requires version %s or newer, current is %s", target, MinUpgradeFrom, currentVersion)
	}

	workingDir := info.Labels[composeWorkingDirLabel]
	if workingDir == "" {
		return fmt.Errorf("container %s is not managed by docker compose (no %s label)", info.Name, composeWorkingDirLabel)
	}

	u.paths.hostDir = workingDir

	if _, err = os.Stat(LocalDeployDir); err != nil {
		return fmt.Errorf("deploy dir is not mounted to %s: %w", LocalDeployDir, err)
	}

	u.journal.Components = append(u.journal.Components, ComponentState{
		Component:    ComponentConfigurer,
		Name:         info.Name,
		OldID:        info.ID,
		OldImage:     info.Image,
		NewImage:     repository + ":" + target,
		RollbackName: fmt.Sprintf("%s-rollback-%s", info.Name, u.opts.UpdateID),
		NewID:        "",
		Stopped:      false,
	})

	return u.journal.Save()
}

// component возвращает состояние компонента из журнала.
func (u *Updater) component(name string) *ComponentState {
	for i := range u.journal.Components {
		if u.journal.Components[i].Component == name {
			return &u.journal.Components[i]
		}
	}

	return nil
}

// backup сохраняет файлы, смонтированные в конфигуратор, и compose-файлы.
func (u *Updater) backup(ctx context.Context, configurer *ComponentState) error {
	info, err := u.controller.GetContainer(ctx, configurer.OldID)
	if err != nil {
		return err
	}

	hostPaths := make([]string, 0, len(info.Mounts))

	for _, mount := range info.Mounts {
		if mount.Type == "bind" {
			hostPaths = append(hostPaths, mount.Source)
		}
	}

	hostPaths = append(hostPaths, composeFiles(info.Labels)...)

	files := make([]string, 0, len(hostPaths))

	for _, hostPath := range hostPaths {
		rel, ok := u.paths.relative(hostPath)
		if !ok {
			continue
		}

		stat, err := os.Stat(u.paths.local(rel))
		if err != nil || !stat.Mode().IsRegular() {
			continue
		}

		files = append(files, rel)
	}

	backups, err := backupFiles(u.paths, u.journal.Dir(), files)
	if err != nil {
		return err
	}

	u.journal.Backups = backups
	u.log.Info("files backed up", "step", stepBackup, "files", files)

	return u.journal.Save()
}

// replace заменяет контейнер конфигуратора новой версией и ждет ее готовности.
func (u *Updater) replace(ctx context.Context, state *ComponentState) error {
	// Флаг ставится до замены: при откате по нему понятно, что контейнер могли остановить.
	state.Stopped = true

	if err := u.journal.Save(); err != nil {
		return err
	}

	result, err := u.controller.ReplaceContainer(ctx, state.Name, state.NewImage, state.RollbackName)
	if err != nil {
		return err
	}

	state.NewID = result.NewID

	if err = u.journal.Save(); err != nil {
		return err
	}

	u.log.Info("container recreated, waiting for health", "step", stepConfigurer, "container", state.Name, "image", state.NewImage)

	if err = u.waitHealthy(ctx, result.NewID, configurerHealthTimeout, u.configurerHealthy); err != nil {
		return err
	}

	u.log.Info("container is healthy", "step", stepConfigurer, "container", state.Name)

	return nil
}

// configurerHealthy проверяет, что новая версия конфигуратора запустилась (миграции прошли).
func (u *Updater) configurerHealthy(ctx context.Context) error {
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}

	if err := getJSON(ctx, u.http, u.opts.ConfigurerHealthURL, &health); err != nil {
		return err
	}

	if health.Status != "ok" || health.Version != u.journal.ToVersion {
		return fmt.Errorf("unexpected health: status=%q version=%q", health.Status, health.Version)
	}

	return nil
}

// updateCompose прописывает новый тег образа в compose-файлы, чтобы docker compose up не откатил версию.
func (u *Updater) updateCompose(image string) error {
	configurer := u.component(ComponentConfigurer)

	info, err := u.controller.GetContainer(context.Background(), configurer.Name)
	if err != nil {
		return err
	}

	repository, tag := splitImage(image)

	for _, file := range composeFiles(info.Labels) {
		rel, ok := u.paths.relative(file)
		if !ok {
			u.log.Warn("compose file is outside deploy dir, skipping", "step", u.journal.Step, "file", file)

			continue
		}

		changed, err := setComposeImageTag(u.paths.local(rel), repository, tag)
		if err != nil {
			return err
		}

		if changed {
			u.log.Info("compose file updated", "step", u.journal.Step, "file", rel, "image", image)
		}
	}

	return nil
}

// commit удаляет старый контейнер и старые бэкапы. После начала фиксации откат не выполняется.
func (u *Updater) commit(ctx context.Context) error {
	u.setStep(stepCommit, "removing previous container")

	for _, component := range u.journal.Components {
		if err := u.controller.CommitContainer(ctx, component.Name, component.RollbackName); err != nil {
			return err
		}
	}

	if err := pruneUpdates(u.updatesDir(), u.opts.UpdateID, keepUpdates); err != nil {
		u.log.Warn("cannot prune old updates", "step", stepCommit, "error", err.Error())
	}

	return nil
}

// finish записывает итог успешного обновления.
func (u *Updater) finish(commitErr error) {
	if commitErr != nil {
		// Новая версия работает, но старый контейнер не удалился — это не повод откатываться.
		u.log.Warn("update succeeded, but cleanup failed", "step", stepCommit, "error", commitErr.Error())
	}

	if err := u.journal.Finish(PhaseSucceeded, nil); err != nil {
		u.log.Error("cannot save journal", "error", err.Error())
	}

	u.result(PhaseSucceeded, nil)
}

// rollbackAndReport откатывает обновление и сообщает результат.
func (u *Updater) rollbackAndReport(ctx context.Context, cause error) {
	u.log.Error("update failed, rolling back", "step", u.journal.Step, "error", cause.Error())

	// Откат не должен прерываться отменой контекста обновления.
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()

	if err := u.rollback(rollbackCtx); err != nil {
		combined := fmt.Errorf("%w; rollback failed: %v", cause, err)

		_ = u.journal.Finish(PhaseFailed, combined)
		u.result(PhaseFailed, combined)

		return
	}

	_ = u.journal.Finish(PhaseRolledBack, cause)
	u.result(PhaseRolledBack, cause)
}

// rollback возвращает исходный контейнер конфигуратора и восстанавливает файлы.
func (u *Updater) rollback(ctx context.Context) error {
	u.journal.Phase = PhaseRollingBack
	u.setStep(stepRollback, "restoring previous state")

	configurer := u.component(ComponentConfigurer)

	// Конфигуратор не останавливался — он продолжает работать, откатывать нечего.
	// Файлы не восстанавливаем: работающий сервис мог менять данные после снятия бэкапа.
	if configurer == nil || !configurer.Stopped {
		return nil
	}

	var errs []error

	if err := u.controller.RestoreContainer(ctx, configurer.Name, configurer.OldID, configurer.RollbackName); err != nil {
		errs = append(errs, err)
	}

	if len(u.journal.Backups) > 0 {
		if err := restoreFiles(u.paths, u.journal.Dir(), u.journal.Backups); err != nil {
			errs = append(errs, err)
		} else {
			u.log.Info("files restored from backup", "step", stepRollback)
		}
	}

	if err := u.controller.StartContainer(ctx, configurer.Name); err != nil {
		errs = append(errs, err)
	} else {
		u.log.Info("previous container started", "step", stepRollback, "container", configurer.Name, "image", configurer.OldImage)
	}

	return errors.Join(errs...)
}

// setStep фиксирует текущий шаг в журнале и в логах.
func (u *Updater) setStep(step, message string) {
	u.journal.Step = step

	if err := u.journal.Save(); err != nil {
		u.log.Error("cannot save journal", "step", step, "error", err.Error())
	}

	u.log.Info(message, "step", step)
}

// result пишет итоговую строку, по которой конфигуратор определяет результат обновления.
func (u *Updater) result(phase string, err error) {
	attrs := []any{"result", phase}

	if u.journal != nil {
		attrs = append(attrs, "from_version", u.journal.FromVersion, "to_version", u.journal.ToVersion)
	}

	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}

	u.log.Info("update finished", attrs...)
}

// composeFiles возвращает пути compose-файлов проекта на хосте из лейблов контейнера.
func composeFiles(labels map[string]string) []string {
	result := make([]string, 0)

	for _, file := range strings.Split(labels[composeConfigFilesLabel], ",") {
		if file = strings.TrimSpace(file); file != "" {
			result = append(result, file)
		}
	}

	return result
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

// errorFromText восстанавливает ошибку из журнала.
func errorFromText(text string) error {
	if text == "" {
		return nil
	}

	return errors.New(text)
}
