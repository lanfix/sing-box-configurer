// Updater — одноразовый процесс обновления, который конфигуратор запускает из новой версии:
// job-контейнером (docker) или transient-службой (systemd). Параметры передаются через переменные окружения.
//
// Логи пишутся в stdout и в файл updater.log папки обновления строками JSON; последняя строка с полем
// "result" содержит итог обновления. Процесс завершается с кодом 0 и при успехе, и после отката:
// результат читается из логов, а ненулевой код означает падение самого updater (его перезапустят,
// и он выполнит откат).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/platform/docker"
	"github.com/lanfix/sing-box-configurer/internal/platform/systemd"
	"github.com/lanfix/sing-box-configurer/internal/updater"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

func main() {
	updateID := os.Getenv("UPDATE_ID")
	platformName := os.Getenv("UPDATE_PLATFORM")

	updatesDir, err := updatesDir(platformName)
	logger := newLogger(updatesDir, updateID)

	logger.Info("updater started", "step", "start", "version", version.Version, "platform", platformName)

	// Сигнал остановки не прерывает обновление посреди шага: откат выполнится при следующем запуске.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var target updater.Target

	if err == nil {
		target, err = newTarget(ctx, platformName, logger)
	}

	var u *updater.Updater

	if err == nil {
		u, err = updater.New(updater.Options{
			UpdateID:   updateID,
			UpdatesDir: updatesDir,
			HealthURL:  os.Getenv("CONFIGURER_HEALTH_URL"),
		}, target, logger)
	}

	if err != nil {
		// Ничего не менялось, повторный запуск не поможет — сообщаем результат и выходим с кодом 0.
		logger.Info("update finished", "result", updater.PhaseFailed, "to_version", version.Version, "error", err.Error())

		return
	}

	u.Run(ctx)
}

// updatesDir возвращает папку журналов обновлений платформы.
func updatesDir(platformName string) (string, error) {
	switch platformName {
	case platform.NameDocker:
		return docker.UpdatesDir(), nil

	case platform.NameSystemd:
		dir := os.Getenv(systemd.EnvUpdatesDir)
		if dir == "" {
			return "", fmt.Errorf("%s is not set", systemd.EnvUpdatesDir)
		}

		return dir, nil

	case "":
		// Прежние версии запускали updater через docker-controller, без UPDATE_PLATFORM.
		if os.Getenv("CONTROLLER_URL") != "" {
			return "", errors.New("this version works without docker-controller: migrate the installation manually (mount /var/run/docker.sock into sing-box-configurer and remove docker-controller from docker-compose.yaml)")
		}
	}

	return "", fmt.Errorf("unknown update platform %q", platformName)
}

// newTarget создает Target платформы по переменным окружения.
func newTarget(ctx context.Context, platformName string, logger *slog.Logger) (updater.Target, error) {
	if platformName == platform.NameDocker {
		return docker.NewTarget(ctx, os.Getenv("CONFIGURER_CONTAINER"), logger)
	}

	var backupPaths []string

	for _, path := range filepath.SplitList(os.Getenv(systemd.EnvBackupPaths)) {
		if path = strings.TrimSpace(path); path != "" {
			backupPaths = append(backupPaths, path)
		}
	}

	return systemd.NewTarget(systemd.TargetOptions{
		Unit:          os.Getenv(systemd.EnvConfigurerUnit),
		Binary:        os.Getenv(systemd.EnvConfigurerBinary),
		BackupPaths:   backupPaths,
		SingBoxUnit:   os.Getenv(systemd.EnvSingBoxUnit),
		SingBoxBinary: os.Getenv(systemd.EnvSingBoxBinary),
	}, logger)
}

// newLogger создает JSON-логгер в stdout и, если известна папка обновления, в ее updater.log.
func newLogger(updatesDir, updateID string) *slog.Logger {
	var output io.Writer = os.Stdout

	if updatesDir != "" {
		if file, err := updater.OpenLog(updatesDir, updateID); err == nil {
			output = io.MultiWriter(os.Stdout, file)
		}
	}

	return slog.New(slog.NewJSONHandler(output, nil))
}
