// Package scheduler выполняет задачи конфигуратора по расписанию.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/cron"
	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// RestartSettings возвращает текущие настройки плановой перезагрузки.
type RestartSettings func() settings.Restart

// RestartStatus — состояние плановой перезагрузки для интерфейса.
type RestartStatus struct {
	settings.Restart

	NextRun   *time.Time `json:"next_run,omitempty"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

// RestartTask перезапускает sing-box по расписанию из общих настроек. Заменяет отдельный контейнер
// cron-scheduler. Расписание перечитывается каждую минуту, поэтому изменения действуют сразу.
type RestartTask struct {
	settings RestartSettings
	restart  func() error

	mu        sync.Mutex
	lastRun   time.Time
	lastError string
}

// NewRestartTask создает задачу. restart перезапускает sing-box.
func NewRestartTask(settings RestartSettings, restart func() error) *RestartTask {
	return &RestartTask{
		settings:  settings,
		restart:   restart,
		mu:        sync.Mutex{},
		lastRun:   time.Time{},
		lastError: "",
	}
}

// Start запускает проверку расписания в начале каждой минуты.
func (t *RestartTask) Start(ctx context.Context) {
	go func() {
		for {
			now := time.Now()
			wait := now.Truncate(time.Minute).Add(time.Minute).Sub(now)

			select {
			case <-ctx.Done():
				return

			case <-time.After(wait):
			}

			t.tick(time.Now().Truncate(time.Minute))
		}
	}()
}

// tick перезапускает sing-box, если расписание срабатывает в минуту minute.
func (t *RestartTask) tick(minute time.Time) {
	config := t.settings()

	if !config.Enabled {
		return
	}

	schedule, location, err := parse(config)
	if err != nil {
		log.Printf("Scheduled restart: invalid settings: %v", err)

		return
	}

	if !schedule.Matches(minute.In(location)) {
		return
	}

	log.Printf("Scheduled restart of sing-box (%s %s)", config.Schedule, config.Timezone)

	err = t.restart()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastRun = minute
	t.lastError = ""

	if err != nil {
		t.lastError = err.Error()

		log.Printf("Scheduled restart failed: %v", err)
	}
}

// Status возвращает настройки, время следующего и последнего запуска.
func (t *RestartTask) Status() RestartStatus {
	config := t.settings()

	status := RestartStatus{
		Restart:   config,
		NextRun:   nil,
		LastRun:   nil,
		LastError: "",
	}

	t.mu.Lock()

	if !t.lastRun.IsZero() {
		lastRun := t.lastRun
		status.LastRun = &lastRun
		status.LastError = t.lastError
	}

	t.mu.Unlock()

	if !config.Enabled {
		return status
	}

	schedule, location, err := parse(config)
	if err != nil {
		return status
	}

	if next := schedule.Next(time.Now().In(location)); !next.IsZero() {
		status.NextRun = &next
	}

	return status
}

// parse разбирает расписание и часовой пояс настроек.
func parse(config settings.Restart) (*cron.Schedule, *time.Location, error) {
	schedule, err := cron.Parse(config.Schedule)
	if err != nil {
		return nil, nil, err
	}

	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return nil, nil, err
	}

	return schedule, location, nil
}
