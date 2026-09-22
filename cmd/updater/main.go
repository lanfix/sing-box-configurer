// Updater — одноразовый процесс обновления, который конфигуратор запускает в отдельном контейнере
// через docker-controller. Параметры передаются через переменные окружения.
//
// Логи пишутся в stdout строками JSON; последняя строка с полем "result" содержит итог обновления.
// Процесс завершается с кодом 0 и при успехе, и после отката: результат читается из логов,
// а ненулевой код означает падение самого updater (docker перезапустит его, и он выполнит откат).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lanfix/sing-box-configurer/internal/updater"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("updater started", "step", "start", "version", version.Version)

	opts := updater.Options{
		UpdateID:            os.Getenv("UPDATE_ID"),
		ConfigurerContainer: os.Getenv("CONFIGURER_CONTAINER"),
		ControllerContainer: os.Getenv("CONTROLLER_CONTAINER"),
		ConfigurerHealthURL: os.Getenv("CONFIGURER_HEALTH_URL"),
		ControllerURL:       os.Getenv("CONTROLLER_URL"),
		ControllerAPIKey:    os.Getenv("CONTROLLER_API_KEY"),
	}

	u, err := updater.New(opts, logger)
	if err != nil {
		// Ничего не менялось, повторный запуск не поможет — сообщаем результат и выходим с кодом 0.
		logger.Info("update finished", "result", updater.PhaseFailed, "error", err.Error())

		return
	}

	// Сигнал остановки не прерывает обновление посреди шага: откат выполнится при следующем запуске.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	u.Run(ctx)
}
