package systemd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/semver"
	"github.com/lanfix/sing-box-configurer/internal/updater"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

const (
	// UpdaterUnit — transient-служба updater. Фиксированное имя не дает запустить два обновления сразу.
	UpdaterUnit = "sing-box-configurer-updater"

	// jobFileName — описание запущенного обновления в его папке.
	jobFileName = "job.json"

	// releaseDirName — папка с бинарниками новой версии внутри папки обновления.
	releaseDirName = "release"

	// maxLogSize ограничивает объем читаемого лога updater.
	maxLogSize = 4 << 20
)

// Переменные окружения updater, специфичные для systemd.
const (
	EnvConfigurerUnit   = "CONFIGURER_UNIT"
	EnvConfigurerBinary = "CONFIGURER_BINARY"
	EnvBackupPaths      = "BACKUP_PATHS"
	EnvUpdatesDir       = "UPDATES_DIR"
	EnvSingBoxUnit      = "SING_BOX_UNIT"
	EnvSingBoxBinary    = "SING_BOX_BINARY"
)

// jobInfo — описание обновления, которое конфигуратор записывает перед запуском updater.
type jobInfo struct {
	UpdateID  string    `json:"update_id"`
	Target    string    `json:"target"`
	StartedAt time.Time `json:"started_at"`
}

// updates обновляет конфигуратор заменой бинарника: архив релиза загружается из GitHub, updater
// из него запускается transient-службой (systemd-run) и переживает перезапуск службы конфигуратора.
type updates struct {
	opts     Options
	releases *releases
}

// Releases возвращает релизы из GitHub Releases.
func (u *updates) Releases(ctx context.Context) ([]platform.Release, error) {
	return u.releases.List(ctx)
}

// Changelog возвращает текст релиза.
func (u *updates) Changelog(ctx context.Context, version string) (string, error) {
	return u.releases.Notes(ctx, version)
}

// Unsupported возвращает причину, по которой обновление через интерфейс недоступно.
func (u *updates) Unsupported(_ context.Context) string {
	if !semver.IsValid(version.Version) {
		return fmt.Sprintf("Сборка %s не является релизом: обновление через интерфейс недоступно, установите новую версию вручную.", version.Version)
	}

	// systemd задает INVOCATION_ID всем процессам служб.
	if os.Getenv("INVOCATION_ID") == "" {
		return "Конфигуратор запущен не службой systemd: обновление через интерфейс недоступно."
	}

	if _, err := releaseArch(); err != nil {
		return "Для этой архитектуры нет сборок релизов: " + err.Error()
	}

	return ""
}

// Start загружает релиз target и запускает updater из него transient-службой.
func (u *updates) Start(ctx context.Context, updateID, target string) (time.Time, error) {
	if reason := u.Unsupported(ctx); reason != "" {
		return time.Time{}, errors.New(reason)
	}

	if isActive(ctx, UpdaterUnit) {
		return time.Time{}, errors.New("update is already in progress")
	}

	// Служба прошлого обновления могла остаться в состоянии failed.
	_, _ = run(ctx, "systemctl", "reset-failed", UpdaterUnit)

	binary, err := executable()
	if err != nil {
		return time.Time{}, err
	}

	dir := filepath.Join(u.opts.UpdatesDir, updateID)
	releaseDir := filepath.Join(dir, releaseDirName)

	if err = u.releases.Download(ctx, target, releaseDir); err != nil {
		return time.Time{}, fmt.Errorf("cannot download %s: %w", target, err)
	}

	job := jobInfo{
		UpdateID:  updateID,
		Target:    target,
		StartedAt: time.Now().UTC(),
	}

	if err = writeJSON(filepath.Join(dir, jobFileName), job); err != nil {
		return time.Time{}, err
	}

	args := []string{
		"--unit=" + UpdaterUnit,
		"--description=sing-box-configurer update to " + target,
		"--collect",
		"--property=Restart=on-failure",
		"--property=RestartSec=5",
		"--setenv=UPDATE_PLATFORM=" + platform.NameSystemd,
		"--setenv=UPDATE_ID=" + updateID,
		"--setenv=CONFIGURER_HEALTH_URL=" + u.opts.HealthURL,
		"--setenv=" + EnvUpdatesDir + "=" + u.opts.UpdatesDir,
		"--setenv=" + EnvConfigurerUnit + "=" + u.opts.ConfigurerUnit,
		"--setenv=" + EnvConfigurerBinary + "=" + binary,
		"--setenv=" + EnvBackupPaths + "=" + strings.Join(u.opts.BackupPaths, string(os.PathListSeparator)),
		"--setenv=" + EnvSingBoxUnit + "=" + u.opts.SingBoxUnit,
		"--setenv=" + EnvSingBoxBinary + "=" + u.opts.SingBoxBinary,
		filepath.Join(releaseDir, "updater"),
	}

	if _, err = run(ctx, "systemd-run", args...); err != nil {
		return time.Time{}, fmt.Errorf("cannot start updater: %w", err)
	}

	log.Printf("Update %s to %s started in unit %s", updateID, target, UpdaterUnit)

	return job.StartedAt, nil
}

// Job возвращает последнее обновление по файлам его папки и состоянию службы updater.
func (u *updates) Job(ctx context.Context) (platform.Job, error) {
	entries, err := os.ReadDir(u.opts.UpdatesDir)
	if errors.Is(err, os.ErrNotExist) {
		return platform.Job{}, nil
	}

	if err != nil {
		return platform.Job{}, fmt.Errorf("cannot read updates dir: %w", err)
	}

	// ID обновлений — время запуска, поэтому новые идут последними по имени.
	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	slices.Sort(names)
	slices.Reverse(names)

	for _, name := range names {
		dir := filepath.Join(u.opts.UpdatesDir, name)

		var job jobInfo

		if readJSON(filepath.Join(dir, jobFileName), &job) != nil {
			continue
		}

		return u.job(ctx, dir, job), nil
	}

	return platform.Job{}, nil
}

// job собирает состояние обновления из папки dir.
func (u *updates) job(ctx context.Context, dir string, job jobInfo) platform.Job {
	result := platform.Job{
		Exists:     true,
		Running:    isActive(ctx, UpdaterUnit),
		UpdateID:   job.UpdateID,
		Target:     job.Target,
		StartedAt:  job.StartedAt.Format(time.RFC3339Nano),
		FinishedAt: "",
		ExitCode:   0,
		Logs:       readLog(filepath.Join(dir, updater.LogFileName)),
	}

	var journal struct {
		FinishedAt *time.Time `json:"finished_at"`
	}

	if readJSON(filepath.Join(dir, "journal.json"), &journal) == nil && journal.FinishedAt != nil {
		result.FinishedAt = journal.FinishedAt.Format(time.RFC3339Nano)
	}

	return result
}

// executable возвращает путь установленного бинарника конфигуратора.
func executable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot find own executable: %w", err)
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	return path, nil
}

// readLog читает лог updater (не больше maxLogSize с конца).
func readLog(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}

	defer func() {
		_ = file.Close()
	}()

	if info, err := file.Stat(); err == nil && info.Size() > maxLogSize {
		_, _ = file.Seek(-maxLogSize, io.SeekEnd)
	}

	data, _ := io.ReadAll(file)

	return string(data)
}

// readJSON читает JSON-файл.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, v)
}

// writeJSON записывает JSON-файл.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0644)
}
