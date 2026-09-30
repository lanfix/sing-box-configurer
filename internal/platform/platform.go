// Package platform описывает способ установки конфигуратора и sing-box (docker, systemd и др.):
// как проверить и перезапустить sing-box и как обновить сам конфигуратор. Реализации — в подпакетах.
package platform

import (
	"context"
	"errors"
	"time"
)

// Названия платформ (поле platform конфига).
const (
	NameDocker  = "docker"
	NameSystemd = "systemd"
)

// ErrNotRunning возвращается, если sing-box не запущен и действие выполнить нельзя.
var ErrNotRunning = errors.New("sing-box is not running")

// Platform — среда, в которой установлены sing-box и конфигуратор.
type Platform struct {
	// Name — название платформы: docker, systemd.
	Name string

	SingBox SingBox
	Updates Updates
}

// SingBox управляет процессом sing-box.
type SingBox interface {
	// Check проверяет конфиг командой sing-box check.
	Check(ctx context.Context, config []byte) (CheckResult, error)

	// Restart перезапускает sing-box.
	Restart(ctx context.Context) error

	// State возвращает состояние процесса sing-box.
	State(ctx context.Context) (State, error)

	// Logs возвращает последние lines строк логов sing-box.
	Logs(ctx context.Context, lines int) (string, error)
}

// CheckResult — результат sing-box check.
type CheckResult struct {
	ExitCode int
	Output   string
}

// State — состояние процесса sing-box.
type State struct {
	// Running — процесс работает.
	Running bool

	// Failed — процесс завершился или перезапускается после падения.
	Failed bool

	// ExitCode — код выхода последнего завершения (если известен).
	ExitCode int
}

// Updates обновляет конфигуратор.
type Updates interface {
	// Releases возвращает опубликованные релизы (без списка изменений).
	Releases(ctx context.Context) ([]Release, error)

	// Changelog возвращает список изменений версии.
	Changelog(ctx context.Context, version string) (string, error)

	// Unsupported возвращает причину, по которой обновление через интерфейс недоступно, или пустую строку.
	Unsupported(ctx context.Context) string

	// Start запускает обновление updateID до версии target и возвращает время запуска.
	Start(ctx context.Context, updateID, target string) (time.Time, error)

	// Job возвращает состояние процесса последнего обновления.
	Job(ctx context.Context) (Job, error)
}

// Release — опубликованная версия конфигуратора.
type Release struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Changelog   string    `json:"changelog"`
}

// Job — процесс обновления (updater).
type Job struct {
	// Exists — обновления уже запускались.
	Exists bool

	Running  bool
	UpdateID string
	Target   string

	StartedAt  string
	FinishedAt string
	ExitCode   int

	// Logs — строки JSON-лога updater.
	Logs string
}
