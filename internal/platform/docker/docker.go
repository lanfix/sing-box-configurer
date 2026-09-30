// Package docker — установка в docker compose: sing-box и конфигуратор работают в контейнерах,
// конфигуратор управляет ими через Docker API (смонтированный /var/run/docker.sock).
package docker

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/dockerapi"
	"github.com/lanfix/sing-box-configurer/internal/platform"
)

var (
	// singBoxLabels — лейблы контейнера sing-box, по которым конфигуратор его находит.
	singBoxLabels = map[string]string{
		"app":     "sing-box",
		"managed": "true",
	}

	// checkCommand проверяет конфиг, переданный на стандартный ввод.
	checkCommand = []string{"sing-box", "check", "--disable-color", "-c", "stdin"}
)

// Options — параметры платформы docker.
type Options struct {
	// ListenPort — порт HTTP-сервера конфигуратора (для проверки новой версии при обновлении).
	ListenPort string
}

// New создает платформу docker. Docker API проверяется при каждом действии: если сокет не смонтирован,
// конфигуратор работает, а ошибка видна при применении конфига и обновлении.
func New(opts Options) (platform.Platform, error) {
	manager, err := dockerapi.NewManager()
	if err != nil {
		return platform.Platform{}, err
	}

	return platform.Platform{
		Name: platform.NameDocker,
		SingBox: &singBox{
			docker: manager,
		},
		Updates: &updates{
			docker:     manager,
			registry:   NewRegistry(),
			listenPort: opts.ListenPort,
		},
	}, nil
}

// singBox управляет контейнером sing-box с лейблами app=sing-box и managed=true.
type singBox struct {
	docker *dockerapi.Manager
}

// Check проверяет конфиг командой sing-box check внутри контейнера sing-box.
func (s *singBox) Check(ctx context.Context, config []byte) (platform.CheckResult, error) {
	container, err := s.find(ctx)
	if err != nil {
		return platform.CheckResult{}, err
	}

	if !container.Running || container.Restarting {
		return platform.CheckResult{}, fmt.Errorf("%w: контейнер %s не запущен, проверить конфиг негде", platform.ErrNotRunning, container.Name)
	}

	result, err := s.docker.Exec(ctx, container.ID, checkCommand, config)
	if err != nil {
		return platform.CheckResult{}, err
	}

	return platform.CheckResult{
		ExitCode: result.ExitCode,
		Output:   strings.TrimSpace(result.Stderr + "\n" + result.Stdout),
	}, nil
}

// Restart перезапускает контейнеры sing-box.
func (s *singBox) Restart(ctx context.Context) error {
	containers, err := s.list(ctx)
	if err != nil {
		return err
	}

	for _, container := range containers {
		if err = s.docker.Restart(ctx, container.ID); err != nil {
			return err
		}
	}

	return nil
}

// State возвращает состояние контейнера sing-box.
func (s *singBox) State(ctx context.Context) (platform.State, error) {
	container, err := s.find(ctx)
	if err != nil {
		return platform.State{}, err
	}

	return platform.State{
		Running:  container.Running && !container.Restarting,
		Failed:   container.Exited(),
		ExitCode: container.ExitCode,
	}, nil
}

// Logs возвращает последние строки логов контейнера sing-box.
func (s *singBox) Logs(ctx context.Context, lines int) (string, error) {
	container, err := s.find(ctx)
	if err != nil {
		return "", err
	}

	return s.docker.Logs(ctx, container.ID, strconv.Itoa(lines))
}

// find возвращает контейнер sing-box, предпочитая запущенный.
func (s *singBox) find(ctx context.Context) (dockerapi.Info, error) {
	containers, err := s.list(ctx)
	if err != nil {
		return dockerapi.Info{}, err
	}

	for _, container := range containers {
		if container.Running && !container.Restarting {
			return container, nil
		}
	}

	return containers[0], nil
}

// list возвращает контейнеры sing-box. Пустой список — ошибка.
func (s *singBox) list(ctx context.Context) ([]dockerapi.Info, error) {
	containers, err := s.docker.List(ctx, dockerapi.Filter{
		ID:     "",
		Name:   "",
		Labels: singBoxLabels,
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось найти контейнер sing-box: %w", err)
	}

	if len(containers) == 0 {
		return nil, errors.New("контейнер sing-box с лейблами app=sing-box и managed=true не найден")
	}

	return containers, nil
}
