package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

func TestRestartTask(t *testing.T) {
	config := settings.Restart{
		Enabled:  true,
		Schedule: "0 6 * * *",
		Timezone: "UTC",
	}

	restarts := 0
	restartErr := error(nil)

	task := NewRestartTask(func() settings.Restart {
		return config
	}, func() error {
		restarts++

		return restartErr
	})

	task.tick(time.Date(2026, 9, 29, 5, 59, 0, 0, time.UTC))
	task.tick(time.Date(2026, 9, 29, 6, 0, 0, 0, time.UTC))

	if restarts != 1 {
		t.Fatalf("restarts = %d, want 1", restarts)
	}

	restartErr = errors.New("controller unavailable")
	task.tick(time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC))

	if status := task.Status(); status.LastError == "" || status.LastRun == nil || status.NextRun == nil {
		t.Errorf("status must contain last error and runs: %+v", status)
	}

	// Выключенная задача не срабатывает и не показывает следующий запуск.
	config.Enabled = false
	task.tick(time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC))

	if restarts != 2 || task.Status().NextRun != nil {
		t.Errorf("disabled task must not run: restarts=%d", restarts)
	}
}
