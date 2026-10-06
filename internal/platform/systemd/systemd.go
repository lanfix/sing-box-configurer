// Package systemd — установка без контейнеров: sing-box и конфигуратор работают службами systemd,
// конфигуратор управляет ими через systemctl (запускается от root).
package systemd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
)

// commandTimeout ограничивает время выполнения systemctl и journalctl.
const commandTimeout = time.Minute

// Options — параметры платформы systemd.
type Options struct {
	// SingBoxUnit — служба sing-box.
	SingBoxUnit string

	// SingBoxBinary — бинарник sing-box (для sing-box check).
	SingBoxBinary string

	// ConfigurerUnit — служба конфигуратора.
	ConfigurerUnit string

	// HealthURL — /api/health конфигуратора, по которому updater проверяет новую версию.
	HealthURL string

	// UpdatesDir — папка журналов, бэкапов и загруженных релизов обновлений.
	UpdatesDir string

	// BackupPaths — файлы и каталоги данных, которые сохраняются перед обновлением.
	BackupPaths []string

	// Repository — репозиторий GitHub с релизами (lanfix/sing-box-configurer).
	Repository string
}

// New создает платформу systemd.
func New(opts Options) platform.Platform {
	return platform.Platform{
		Name: platform.NameSystemd,
		SingBox: &singBox{
			unit:   opts.SingBoxUnit,
			binary: opts.SingBoxBinary,
		},
		Updates: &updates{
			opts:     opts,
			releases: newReleases(opts.Repository),
		},
		Logs: &logs{
			units: map[string]string{
				platform.LogSourceSingBox:    opts.SingBoxUnit,
				platform.LogSourceConfigurer: opts.ConfigurerUnit,
			},
		},
		Network: network{},
	}
}

// logs читает журналы служб sing-box и конфигуратора из journald.
type logs struct {
	// units — служба каждого источника журнала.
	units map[string]string
}

// Read возвращает последние lines строк журнала службы источника source.
func (l *logs) Read(ctx context.Context, source string, lines int) (string, error) {
	unit, ok := l.units[source]
	if !ok {
		return "", fmt.Errorf("%w: %s", platform.ErrUnknownLogSource, source)
	}

	return unitLogs(ctx, unit, lines, time.Time{})
}

// singBox управляет службой sing-box.
type singBox struct {
	unit   string
	binary string
}

// Check проверяет конфиг командой sing-box check, передавая его на стандартный ввод.
func (s *singBox) Check(ctx context.Context, config []byte) (platform.CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	var output bytes.Buffer

	cmd := exec.CommandContext(ctx, s.binary, "check", "--disable-color", "-c", "stdin")
	cmd.Stdin = bytes.NewReader(config)
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	var exitErr *exec.ExitError

	if err != nil && !errors.As(err, &exitErr) {
		return platform.CheckResult{}, fmt.Errorf("cannot run %s check: %w", s.binary, err)
	}

	return platform.CheckResult{
		ExitCode: cmd.ProcessState.ExitCode(),
		Output:   strings.TrimSpace(output.String()),
	}, nil
}

// Restart перезапускает службу sing-box.
func (s *singBox) Restart(ctx context.Context) error {
	return restartUnit(ctx, s.unit)
}

// State возвращает состояние службы sing-box.
func (s *singBox) State(ctx context.Context) (platform.State, error) {
	return unitState(ctx, s.unit)
}

// Logs возвращает последние строки журнала службы sing-box.
func (s *singBox) Logs(ctx context.Context, lines int) (string, error) {
	return unitLogs(ctx, s.unit, lines, time.Time{})
}

// network читает сеть хоста командой ip (конфигуратор работает на том же хосте, что и sing-box).
type network struct{}

// Read возвращает адреса интерфейсов и таблицу соседей хоста.
func (network) Read(ctx context.Context) (platform.NetworkOutput, error) {
	addresses, err := run(ctx, "ip", "-o", "addr", "show")
	if err != nil {
		return platform.NetworkOutput{}, err
	}

	neighbors, err := run(ctx, "ip", "-4", "neigh", "show")
	if err != nil {
		return platform.NetworkOutput{}, err
	}

	// Без IPv6 на хосте таблицы соседей IPv6 нет — это не ошибка.
	if neighbors6, err := run(ctx, "ip", "-6", "neigh", "show"); err == nil {
		neighbors += "\n" + neighbors6
	}

	return platform.NetworkOutput{
		Addresses: addresses,
		Neighbors: neighbors,
	}, nil
}

// restartUnit перезапускает службу.
func restartUnit(ctx context.Context, unit string) error {
	if _, err := run(ctx, "systemctl", "restart", unit); err != nil {
		return fmt.Errorf("cannot restart %s: %w", unit, err)
	}

	return nil
}

// unitState возвращает состояние службы по systemctl show.
func unitState(ctx context.Context, unit string) (platform.State, error) {
	output, err := run(ctx, "systemctl", "show", unit, "--property=ActiveState,SubState,ExecMainStatus")
	if err != nil {
		return platform.State{}, fmt.Errorf("cannot get state of %s: %w", unit, err)
	}

	return parseState(output), nil
}

// parseState разбирает вывод systemctl show (строки вида Key=Value).
func parseState(output string) platform.State {
	properties := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		if key, value, ok := strings.Cut(scanner.Text(), "="); ok {
			properties[key] = value
		}
	}

	active, sub := properties["ActiveState"], properties["SubState"]
	exitCode, _ := strconv.Atoi(properties["ExecMainStatus"])

	return platform.State{
		Running:  active == "active" && sub == "running",
		Failed:   active == "failed" || sub == "auto-restart",
		ExitCode: exitCode,
	}
}

// unitLogs возвращает последние строки журнала службы. Если since не нулевое, только записи после него.
func unitLogs(ctx context.Context, unit string, lines int, since time.Time) (string, error) {
	args := []string{"--unit=" + unit, "--lines=" + strconv.Itoa(lines), "--no-pager", "--output=cat"}

	if !since.IsZero() {
		args = append(args, "--since=@"+strconv.FormatInt(since.Unix(), 10))
	}

	output, err := run(ctx, "journalctl", args...)
	if err != nil {
		return "", fmt.Errorf("cannot read logs of %s: %w", unit, err)
	}

	return output, nil
}

// isActive проверяет, что служба запущена или запускается.
func isActive(ctx context.Context, unit string) bool {
	// systemctl is-active завершается с ненулевым кодом для неактивных служб — ошибка не важна.
	output, _ := run(ctx, "systemctl", "is-active", unit)

	switch strings.TrimSpace(output) {
	case "active", "activating", "deactivating", "reloading":
		return true
	}

	return false
}

// run выполняет команду и возвращает ее вывод (stdout и stderr).
func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}
