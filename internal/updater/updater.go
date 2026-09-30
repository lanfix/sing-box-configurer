// Package updater обновляет sing-box-configurer.
//
// Updater — отдельный процесс из новой версии конфигуратора (job-контейнер в docker, transient unit
// в systemd), поэтому логика обновления всегда соответствует версии, на которую выполняется обновление.
// Общий ход обновления (журнал, бэкап файлов, проверка новой версии, откат) описан здесь, а действия,
// зависящие от способа установки, выполняет Target платформы. Каждое действие записывается в журнал;
// при ошибке или после падения updater возвращает прежнюю версию и восстанавливает файлы из бэкапа.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

// MinUpgradeFrom — минимальная версия, с которой можно обновиться на эту. Пустая — без ограничений.
const MinUpgradeFrom = ""

// Шаги обновления (пишутся в журнал и логи).
const (
	StepPrepare  = "prepare"
	StepDownload = "download"
	StepBackup   = "backup"
	StepReplace  = "update-configurer"
	StepFinish   = "finish"
	StepCommit   = "commit"
	StepRollback = "rollback"
)

const (
	// UpdatesDirName — папка с журналами и бэкапами обновлений.
	UpdatesDirName = ".updates"

	// LogFileName — копия JSON-лога updater в папке обновления.
	LogFileName = "updater.log"

	// Сколько папок обновлений хранить.
	keepUpdates = 5

	healthTimeout = 2 * time.Minute
	pollInterval  = 2 * time.Second

	maxFailureLogsLength = 4000
)

// Target — установленный конфигуратор, который заменяется новой версией. Реализуется платформой.
// Данные для отката Target хранит в journal.State: журнал сохраняется после каждого шага.
type Target interface {
	// Root возвращает каталог, относительно которого заданы пути файлов для бэкапа.
	Root() string

	// Prepare проверяет возможность обновления, записывает в journal.FromVersion текущую версию
	// и возвращает файлы и каталоги данных для бэкапа (пути относительно Root).
	Prepare(ctx context.Context, journal *Journal) ([]string, error)

	// Download загружает новую версию.
	Download(ctx context.Context, journal *Journal) error

	// Replace останавливает текущую версию и запускает новую.
	Replace(ctx context.Context, journal *Journal) error

	// Alive возвращает ошибку, если новая версия завершилась или перезапускается после падения.
	Alive(ctx context.Context, journal *Journal) error

	// Logs возвращает последние строки логов новой версии для текста ошибки.
	Logs(ctx context.Context, journal *Journal) string

	// Finish выполняется после успешной проверки новой версии (например, правит compose-файл).
	Finish(ctx context.Context, journal *Journal) error

	// Restore возвращает прежнюю версию на место, не запуская ее.
	Restore(ctx context.Context, journal *Journal) error

	// StartPrevious запускает прежнюю версию после восстановления файлов.
	StartPrevious(ctx context.Context, journal *Journal) error

	// Commit удаляет прежнюю версию, сохраненную для отката.
	Commit(ctx context.Context, journal *Journal) error
}

// Options — параметры обновления.
type Options struct {
	UpdateID string

	// UpdatesDir — папка журналов и бэкапов обновлений.
	UpdatesDir string

	// HealthURL — /api/health новой версии конфигуратора.
	HealthURL string
}

// Updater выполняет обновление.
type Updater struct {
	opts    Options
	target  Target
	http    *http.Client
	log     *slog.Logger
	journal *Journal
}

// New создает updater.
func New(opts Options, target Target, logger *slog.Logger) (*Updater, error) {
	if err := ValidateID(opts.UpdateID); err != nil {
		return nil, err
	}

	if opts.UpdatesDir == "" || opts.HealthURL == "" {
		return nil, errors.New("updates dir and health url are required")
	}

	return &Updater{
		opts:   opts,
		target: target,
		http: &http.Client{
			Timeout: 3 * time.Second,
		},
		log:     logger,
		journal: nil,
	}, nil
}

// ValidateID проверяет ID обновления: он становится именем папки.
func ValidateID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return fmt.Errorf("invalid update id %q", id)
	}

	return nil
}

// Run выполняет обновление. Если журнал с таким ID уже есть (updater перезапустился после падения),
// незавершенное обновление откатывается, а завершенное — только повторно сообщает результат.
func (u *Updater) Run(ctx context.Context) {
	journal, err := loadJournal(u.opts.UpdatesDir, u.opts.UpdateID)
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

		case journal.Step == StepCommit:
			// Новая версия уже прошла проверку — завершаем фиксацию.
			u.log.Warn("updater restarted during commit, finishing commit", "step", StepCommit)
			u.finish(u.commit(ctx))

		default:
			u.log.Warn("updater restarted after crash, rolling back", "step", journal.Step)
			u.rollbackAndReport(ctx, fmt.Errorf("updater was interrupted at step %q", journal.Step))
		}

		return
	}

	u.journal = newJournal(u.opts.UpdatesDir, u.opts.UpdateID)

	if err = u.update(ctx); err != nil {
		u.rollbackAndReport(ctx, err)

		return
	}

	u.finish(u.commit(ctx))
}

// update выполняет обновление до фиксации.
func (u *Updater) update(ctx context.Context) error {
	files, err := u.prepare(ctx)
	if err != nil {
		return err
	}

	u.setStep(StepDownload, "downloading "+u.journal.ToVersion)

	if err = u.target.Download(ctx, u.journal); err != nil {
		return err
	}

	u.setStep(StepBackup, "backing up data files")

	if err = u.backup(files); err != nil {
		return err
	}

	u.setStep(StepReplace, fmt.Sprintf("updating sing-box-configurer %s -> %s", u.journal.FromVersion, u.journal.ToVersion))

	// Флаг ставится до замены: при откате по нему понятно, что прежнюю версию могли остановить.
	u.journal.Stopped = true

	if err = u.journal.Save(); err != nil {
		return err
	}

	replaceErr := u.target.Replace(ctx, u.journal)

	// Журнал сохраняется и при ошибке: в State могут быть данные, нужные для отката.
	if err = u.journal.Save(); err != nil {
		return errors.Join(replaceErr, err)
	}

	if replaceErr != nil {
		return fmt.Errorf("sing-box-configurer update failed: %w", replaceErr)
	}

	u.log.Info("new version started, waiting for health", "step", StepReplace)

	if err = u.waitHealthy(ctx); err != nil {
		return fmt.Errorf("sing-box-configurer update failed: %w", err)
	}

	u.log.Info("new version is healthy", "step", StepReplace)
	u.setStep(StepFinish, "finishing installation")

	if err = u.target.Finish(ctx, u.journal); err != nil {
		return err
	}

	return u.journal.Save()
}

// prepare проверяет возможность обновления и возвращает файлы для бэкапа.
func (u *Updater) prepare(ctx context.Context) ([]string, error) {
	u.setStep(StepPrepare, "checking current state")

	u.journal.ToVersion = version.Version

	files, err := u.target.Prepare(ctx, u.journal)

	// Версии сохраняются и при ошибке — они попадают в итог обновления.
	if saveErr := u.journal.Save(); saveErr != nil && err == nil {
		err = saveErr
	}

	if err != nil {
		return nil, err
	}

	target, current := u.journal.ToVersion, u.journal.FromVersion

	if !semver.IsValid(target) {
		return nil, fmt.Errorf("updater version %q is not a release version", target)
	}

	if semver.Compare(target, current) <= 0 {
		return nil, fmt.Errorf("target version %s is not newer than current %s", target, current)
	}

	if MinUpgradeFrom != "" && semver.Compare(current, MinUpgradeFrom) < 0 {
		return nil, fmt.Errorf("upgrade to %s requires version %s or newer, current is %s", target, MinUpgradeFrom, current)
	}

	return files, nil
}

// backup сохраняет файлы и каталоги данных в папку обновления.
func (u *Updater) backup(paths []string) error {
	files := expandFiles(u.target.Root(), paths)

	backups, err := backupFiles(u.target.Root(), u.journal.Dir(), files)
	if err != nil {
		return err
	}

	u.journal.Backups = backups
	u.log.Info("files backed up", "step", StepBackup, "files", files)

	return u.journal.Save()
}

// waitHealthy ждет, пока новая версия ответит на /api/health. Если она упала или ушла в цикл
// перезапусков, ожидание прерывается, а в ошибку добавляются последние строки ее логов.
func (u *Updater) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(healthTimeout)

	var lastErr error

	for time.Now().Before(deadline) {
		if err := u.target.Alive(ctx, u.journal); err != nil {
			return fmt.Errorf("%w%s", err, u.logsSuffix(ctx))
		}

		if lastErr = u.configurerHealthy(ctx); lastErr == nil {
			return nil
		}

		if err := sleep(ctx, pollInterval); err != nil {
			return err
		}
	}

	return fmt.Errorf("health check timeout after %s: %v%s", healthTimeout, lastErr, u.logsSuffix(ctx))
}

// configurerHealthy проверяет, что запустилась новая версия конфигуратора (миграции прошли).
func (u *Updater) configurerHealthy(ctx context.Context) error {
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}

	if err := getJSON(ctx, u.http, u.opts.HealthURL, &health); err != nil {
		return err
	}

	if health.Status != "ok" || health.Version != u.journal.ToVersion {
		return fmt.Errorf("unexpected health: status=%q version=%q", health.Status, health.Version)
	}

	return nil
}

// logsSuffix возвращает последние строки логов новой версии для текста ошибки.
func (u *Updater) logsSuffix(ctx context.Context) string {
	logs := strings.TrimSpace(u.target.Logs(ctx, u.journal))
	if logs == "" {
		return ""
	}

	if len(logs) > maxFailureLogsLength {
		logs = "..." + logs[len(logs)-maxFailureLogsLength:]
	}

	return "\nlogs:\n" + logs
}

// commit удаляет прежнюю версию и старые бэкапы. После начала фиксации откат не выполняется.
func (u *Updater) commit(ctx context.Context) error {
	u.setStep(StepCommit, "removing previous version")

	if err := u.target.Commit(ctx, u.journal); err != nil {
		return err
	}

	if err := pruneUpdates(u.opts.UpdatesDir, u.opts.UpdateID, keepUpdates); err != nil {
		u.log.Warn("cannot prune old updates", "step", StepCommit, "error", err.Error())
	}

	return nil
}

// finish записывает итог успешного обновления.
func (u *Updater) finish(commitErr error) {
	if commitErr != nil {
		// Новая версия работает, но прежняя не удалилась — это не повод откатываться.
		u.log.Warn("update succeeded, but cleanup failed", "step", StepCommit, "error", commitErr.Error())
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

// rollback возвращает прежнюю версию и восстанавливает файлы.
func (u *Updater) rollback(ctx context.Context) error {
	u.journal.Phase = PhaseRollingBack
	u.setStep(StepRollback, "restoring previous state")

	// Прежняя версия не останавливалась — она продолжает работать, откатывать нечего.
	// Файлы не восстанавливаем: работающий сервис мог менять данные после снятия бэкапа.
	if !u.journal.Stopped {
		return nil
	}

	var errs []error

	if err := u.target.Restore(ctx, u.journal); err != nil {
		errs = append(errs, err)
	}

	if len(u.journal.Backups) > 0 {
		if err := restoreFiles(u.target.Root(), u.journal.Dir(), u.journal.Backups); err != nil {
			errs = append(errs, err)
		} else {
			u.log.Info("files restored from backup", "step", StepRollback)
		}
	}

	if err := u.target.StartPrevious(ctx, u.journal); err != nil {
		errs = append(errs, err)
	} else {
		u.log.Info("previous version started", "step", StepRollback, "version", u.journal.FromVersion)
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

// OpenLog открывает копию лога updater в папке обновления (дописывает: updater может перезапускаться).
func OpenLog(updatesDir, id string) (*os.File, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}

	dir := filepath.Join(updatesDir, id)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create update dir: %w", err)
	}

	return os.OpenFile(filepath.Join(dir, LogFileName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
}

// getJSON выполняет GET-запрос и раскладывает JSON-ответ в v.
func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// sleep ждет d или отмены контекста.
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-time.After(d):
		return nil
	}
}

// errorFromText восстанавливает ошибку из журнала.
func errorFromText(text string) error {
	if text == "" {
		return nil
	}

	return errors.New(text)
}
