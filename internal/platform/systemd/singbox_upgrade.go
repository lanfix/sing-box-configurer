package systemd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/updater"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

// Ключи состояния обновления sing-box в журнале.
const (
	stateSingBoxNew       = "sing_box_new_binary"
	stateSingBoxPrevious  = "sing_box_previous_binary"
	stateSingBoxInstalled = "sing_box_installed"
)

const (
	// Значения по умолчанию из install-systemd.sh: прежние версии конфигуратора не передают их updater.
	defaultSingBoxUnit   = "sing-box"
	defaultSingBoxBinary = "/usr/local/bin/sing-box"

	// singBoxStableTime — сколько новый sing-box должен проработать без падения.
	singBoxStableTime = 10 * time.Second
)

// prepareSingBox решает, нужно ли обновить sing-box: только sing-box-lx старше version.SingBoxVersion.
func (t *Target) prepareSingBox(ctx context.Context, journal *updater.Journal) {
	current, err := singBoxVersion(ctx, t.opts.SingBoxBinary)
	if err != nil {
		t.log.Warn("cannot get sing-box version, skipping sing-box update", "step", updater.StepPrepare, "error", err.Error())

		return
	}

	if cmp, ok := version.CompareSingBox(current, version.SingBoxVersion); !ok || cmp >= 0 {
		t.log.Info("sing-box is up to date or custom", "step", updater.StepPrepare, "version", current, "required", version.SingBoxVersion)

		return
	}

	journal.State[stateSingBoxNew] = filepath.Join(journal.Dir(), "sing-box.new", "sing-box")
	journal.State[stateSingBoxPrevious] = filepath.Join(journal.Dir(), "sing-box.previous")
}

// downloadSingBox загружает архив sing-box-lx из GitHub Releases и проверяет его SHA-256.
func (t *Target) downloadSingBox(ctx context.Context, journal *updater.Journal) error {
	binary := journal.State[stateSingBoxNew]
	if binary == "" {
		return nil
	}

	arch, err := releaseArch()
	if err != nil {
		return err
	}

	name := fmt.Sprintf("sing-box-%s-linux-%s.tar.gz", strings.TrimPrefix(version.SingBoxVersion, "v"), arch)

	t.log.Info("downloading "+name, "step", updater.StepDownload)

	archive, err := newReleases(version.SingBoxReleaseRepository).downloadVerified(ctx, version.SingBoxVersion, name)
	if err != nil {
		return fmt.Errorf("cannot download sing-box: %w", err)
	}

	if err = extractBinaries(archive, filepath.Dir(binary), []string{"sing-box"}); err != nil {
		return err
	}

	got, err := singBoxVersion(ctx, binary)
	if err != nil {
		return err
	}

	if cmp, ok := version.CompareSingBox(got, version.SingBoxVersion); !ok || cmp != 0 {
		return fmt.Errorf("new sing-box reports version %s, want %s", got, version.SingBoxVersion)
	}

	return nil
}

// upgradeSingBox заменяет бинарник sing-box и перезапускает его службу после проверки новой версии конфигуратора.
// Если новый sing-box не запустился, возвращается прежний: конфигуратор работает и с ним.
func (t *Target) upgradeSingBox(ctx context.Context, journal *updater.Journal) error {
	binary := journal.State[stateSingBoxNew]
	if binary == "" {
		return nil
	}

	installed, previous := t.opts.SingBoxBinary, journal.State[stateSingBoxPrevious]

	if err := copyFile(installed, previous); err != nil {
		return fmt.Errorf("cannot save previous sing-box: %w", err)
	}

	t.log.Info("updating sing-box to "+version.SingBoxVersion, "step", updater.StepFinish, "binary", installed, "unit", t.opts.SingBoxUnit)

	err := installBinary(binary, installed)
	if err == nil {
		err = t.restartSingBox(ctx)
	}

	if err == nil {
		journal.State[stateSingBoxInstalled] = "true"

		return nil
	}

	t.log.Warn("sing-box update failed, restoring previous binary", "step", updater.StepFinish, "error", err.Error())

	if restoreErr := installBinary(previous, installed); restoreErr != nil {
		return fmt.Errorf("cannot restore sing-box after failed update: %w", restoreErr)
	}

	return restartUnit(ctx, t.opts.SingBoxUnit)
}

// restartSingBox перезапускает службу sing-box и ждет, пока она проработает singBoxStableTime без падения.
func (t *Target) restartSingBox(ctx context.Context) error {
	if err := restartUnit(ctx, t.opts.SingBoxUnit); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-time.After(singBoxStableTime):
	}

	state, err := unitState(ctx, t.opts.SingBoxUnit)
	if err != nil {
		return err
	}

	if state.Failed || !state.Running {
		logs, _ := unitLogs(ctx, t.opts.SingBoxUnit, 20, time.Time{})

		return fmt.Errorf("service %s is not running (code %d):\n%s", t.opts.SingBoxUnit, state.ExitCode, strings.TrimSpace(logs))
	}

	return nil
}

// commitSingBox удаляет сохраненный прежний бинарник sing-box и загруженный архив.
func (t *Target) commitSingBox(journal *updater.Journal) error {
	var errs []error

	for _, path := range []string{journal.State[stateSingBoxPrevious], filepath.Dir(journal.State[stateSingBoxNew])} {
		if path == "" || path == "." {
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// singBoxVersion возвращает версию бинарника sing-box из первой строки sing-box version.
func singBoxVersion(ctx context.Context, binary string) (string, error) {
	output, err := exec.CommandContext(ctx, binary, "version").Output()
	if err != nil {
		return "", fmt.Errorf("cannot get version of %s: %w", binary, err)
	}

	line, _, _ := strings.Cut(string(output), "\n")
	fields := strings.Fields(line)

	if len(fields) < 3 || fields[1] != "version" {
		return "", fmt.Errorf("unexpected output of %s version: %q", binary, line)
	}

	return fields[2], nil
}
