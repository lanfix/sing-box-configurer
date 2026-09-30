package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/updater"
)

// Ключи состояния в журнале обновления.
const (
	stateBinary    = "binary"
	stateNew       = "new_binary"
	statePrevious  = "previous_binary"
	stateInstalled = "installed"

	// stateReplacedAt — время замены бинарника (Unix): логи новой версии читаются с него.
	stateReplacedAt = "replaced_at"
)

// TargetOptions — параметры Target (передаются updater через переменные окружения).
type TargetOptions struct {
	// Unit — служба конфигуратора.
	Unit string

	// Binary — установленный бинарник конфигуратора.
	Binary string

	// BackupPaths — абсолютные пути файлов и каталогов данных для бэкапа.
	BackupPaths []string
}

// Target заменяет бинарник конфигуратора новой версией и перезапускает его службу. Прежний бинарник
// сохраняется в папке обновления до фиксации. Работает внутри transient-службы updater.
type Target struct {
	opts TargetOptions
	log  *slog.Logger
}

// NewTarget создает Target.
func NewTarget(opts TargetOptions, logger *slog.Logger) (*Target, error) {
	if opts.Unit == "" || opts.Binary == "" {
		return nil, errors.New("configurer unit and binary are required")
	}

	if !filepath.IsAbs(opts.Binary) {
		return nil, fmt.Errorf("configurer binary %q must be an absolute path", opts.Binary)
	}

	return &Target{
		opts: opts,
		log:  logger,
	}, nil
}

// Root возвращает корень файловой системы: пути для бэкапа абсолютные.
func (t *Target) Root() string {
	return "/"
}

// Prepare определяет текущую версию по установленному бинарнику и возвращает пути данных для бэкапа.
func (t *Target) Prepare(ctx context.Context, journal *updater.Journal) ([]string, error) {
	current, err := binaryVersion(ctx, t.opts.Binary)
	if err != nil {
		return nil, err
	}

	journal.FromVersion = current

	self, err := executable()
	if err != nil {
		return nil, err
	}

	journal.State[stateBinary] = t.opts.Binary
	journal.State[stateNew] = filepath.Join(filepath.Dir(self), "sing-box-configurer")
	journal.State[statePrevious] = filepath.Join(journal.Dir(), "sing-box-configurer.previous")

	files := make([]string, 0, len(t.opts.BackupPaths))

	for _, path := range t.opts.BackupPaths {
		if rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/"); rel != "" && filepath.IsAbs(path) {
			files = append(files, rel)
		}
	}

	return files, nil
}

// Download проверяет бинарник новой версии: конфигуратор загрузил его из релиза вместе с updater.
func (t *Target) Download(ctx context.Context, journal *updater.Journal) error {
	got, err := binaryVersion(ctx, journal.State[stateNew])
	if err != nil {
		return err
	}

	if got != journal.ToVersion {
		return fmt.Errorf("new binary reports version %s, want %s", got, journal.ToVersion)
	}

	return nil
}

// Replace сохраняет прежний бинарник, устанавливает новый и перезапускает службу конфигуратора.
func (t *Target) Replace(ctx context.Context, journal *updater.Journal) error {
	binary := journal.State[stateBinary]

	if err := copyFile(binary, journal.State[statePrevious]); err != nil {
		return fmt.Errorf("cannot save previous binary: %w", err)
	}

	if err := installBinary(journal.State[stateNew], binary); err != nil {
		return err
	}

	journal.State[stateInstalled] = "true"
	journal.State[stateReplacedAt] = strconv.FormatInt(time.Now().Unix(), 10)

	t.log.Info("binary installed, restarting service", "step", updater.StepReplace, "binary", binary, "unit", t.opts.Unit)

	return restartUnit(ctx, t.opts.Unit)
}

// Alive проверяет, что служба конфигуратора не упала.
func (t *Target) Alive(ctx context.Context, _ *updater.Journal) error {
	state, err := unitState(ctx, t.opts.Unit)
	if err != nil {
		return err
	}

	if state.Failed {
		return fmt.Errorf("service %s failed with code %d", t.opts.Unit, state.ExitCode)
	}

	return nil
}

// Logs возвращает последние строки журнала службы конфигуратора после замены бинарника.
func (t *Target) Logs(ctx context.Context, journal *updater.Journal) string {
	var since time.Time

	if unix, err := strconv.ParseInt(journal.State[stateReplacedAt], 10, 64); err == nil {
		since = time.Unix(unix, 0)
	}

	logs, err := unitLogs(ctx, t.opts.Unit, 40, since)
	if err != nil {
		return ""
	}

	return logs
}

// Finish ничего не делает: служба уже запущена с новым бинарником.
func (t *Target) Finish(_ context.Context, _ *updater.Journal) error {
	return nil
}

// Restore возвращает прежний бинарник.
func (t *Target) Restore(_ context.Context, journal *updater.Journal) error {
	if journal.State[stateInstalled] != "true" {
		return nil
	}

	return installBinary(journal.State[statePrevious], journal.State[stateBinary])
}

// StartPrevious перезапускает службу конфигуратора с прежним бинарником.
func (t *Target) StartPrevious(ctx context.Context, _ *updater.Journal) error {
	return restartUnit(ctx, t.opts.Unit)
}

// Commit удаляет сохраненный прежний бинарник и копию нового из папки релиза.
func (t *Target) Commit(_ context.Context, journal *updater.Journal) error {
	var errs []error

	for _, path := range []string{journal.State[statePrevious], journal.State[stateNew]} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// binaryVersion возвращает версию бинарника конфигуратора (флаг -version).
func binaryVersion(ctx context.Context, binary string) (string, error) {
	output, err := exec.CommandContext(ctx, binary, "-version").Output()
	if err != nil {
		return "", fmt.Errorf("cannot get version of %s: %w", binary, err)
	}

	return strings.TrimSpace(string(output)), nil
}

// installBinary атомарно заменяет target копией source: файл копируется рядом с target
// и переименовывается поверх него (работающий процесс продолжает использовать старый inode).
func installBinary(source, target string) error {
	tmp := target + ".new"

	if err := copyFile(source, tmp); err != nil {
		return fmt.Errorf("cannot copy %s: %w", source, err)
	}

	if err := os.Chmod(tmp, 0755); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("cannot chmod %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("cannot install %s: %w", target, err)
	}

	return nil
}

// copyFile копирует файл с сохранением прав.
func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}

	defer func() {
		_ = in.Close()
	}()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()

		return err
	}

	if err = out.Sync(); err != nil {
		_ = out.Close()

		return err
	}

	return out.Close()
}
