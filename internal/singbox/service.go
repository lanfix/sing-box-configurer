// Package singbox рендерит конфиг sing-box из данных app.json, сравнивает его с рабочим конфигом
// и применяет: проверяет командой sing-box check, атомарно заменяет рабочий конфиг и перезапускает
// sing-box, а при неудачном запуске возвращает прежний конфиг.
package singbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

const (
	// Сколько ждать запуска sing-box после перезапуска контейнера.
	startTimeout = 30 * time.Second

	// Сколько контейнер должен проработать, чтобы считаться запущенным, если Clash API недоступен.
	stableRunTime = 5 * time.Second

	// Сколько строк логов sing-box показывать при неудачном запуске.
	logsTail = "30"
)

var (
	// ErrNoChanges — отрендеренный конфиг совпадает с рабочим.
	ErrNoChanges = errors.New("конфиг не изменился")

	// ErrCheckFailed — sing-box check отклонил конфиг.
	ErrCheckFailed = errors.New("sing-box check отклонил конфиг")

	// containerLabels — лейблы контейнера sing-box.
	containerLabels = map[string]string{
		"app":     "sing-box",
		"managed": "true",
	}

	// checkCommand проверяет конфиг, переданный на стандартный ввод.
	checkCommand = []string{"sing-box", "check", "--disable-color", "-c", "stdin"}
)

// InputSource собирает данные для рендера из менеджеров приложения.
type InputSource func() (render.Input, error)

// State — отрендеренный и рабочий конфиги для страницы «Конфиг».
type State struct {
	Rendered string   `json:"rendered"`
	Actual   string   `json:"actual"`
	Changed  bool     `json:"changed"`
	Warnings []string `json:"warnings"`

	// ActualError — рабочий конфиг не прочитан или не разобран, Actual содержит его как есть.
	ActualError string `json:"actual_error,omitempty"`
}

// ApplyResult — итог применения конфига.
type ApplyResult struct {
	Message  string   `json:"message"`
	Warnings []string `json:"warnings"`
	Backup   string   `json:"backup,omitempty"`
}

// Service рендерит и применяет конфиг sing-box.
type Service struct {
	source     InputSource
	provider   *singboxconfig.Provider
	controller *dockercontroller.Provider
	clash      *singboxclashapi.ClashAPI

	// Применения выполняются по одному.
	mu sync.Mutex
}

// NewService создает сервис.
func NewService(source InputSource, provider *singboxconfig.Provider, controller *dockercontroller.Provider, clash *singboxclashapi.ClashAPI) *Service {
	return &Service{
		source:     source,
		provider:   provider,
		controller: controller,
		clash:      clash,
		mu:         sync.Mutex{},
	}
}

// Render рендерит конфиг и возвращает его в том виде, в котором он будет записан на диск.
func (s *Service) Render() ([]byte, []string, error) {
	input, err := s.source()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot collect render input: %w", err)
	}

	result, err := render.Render(input)
	if err != nil {
		return nil, nil, err
	}

	data, err := singboxconfig.Marshal(result.Config)
	if err != nil {
		return nil, nil, err
	}

	return data, result.Warnings, nil
}

// Candidates возвращает outbound-ы, из которых urltest-ы подбирают участников при рендере.
func (s *Service) Candidates() ([]outbound.Candidate, error) {
	input, err := s.source()
	if err != nil {
		return nil, fmt.Errorf("cannot collect render input: %w", err)
	}

	return render.Candidates(input), nil
}

// ActualConfig возвращает разобранный рабочий конфиг — тот, с которым запущен sing-box.
func (s *Service) ActualConfig() (map[string]any, error) {
	return s.provider.GetActualConfigParsed()
}

// ProxyClusters возвращает подпись подписки («Happ · профиль») по тегу каждого ее outbound-а.
func (s *Service) ProxyClusters() (map[string]string, error) {
	input, err := s.source()
	if err != nil {
		return nil, fmt.Errorf("cannot collect render input: %w", err)
	}

	clusters := map[string]string{}

	for _, subscription := range input.Subscriptions {
		label := subscription.ProfileName

		switch subscription.Source {
		case outbound.SourceHapp:
			label = "Happ · " + subscription.ProfileName

		case outbound.SourceAmnezia:
			label = "Amnezia · " + subscription.ProfileName
		}

		for _, config := range subscription.Outbounds {
			if tag, _ := config["tag"].(string); tag != "" {
				clusters[tag] = label
			}
		}
	}

	return clusters, nil
}

// State возвращает отрендеренный конфиг, рабочий конфиг в нормализованном виде и признак различий.
func (s *Service) State() (*State, error) {
	rendered, warnings, err := s.Render()
	if err != nil {
		return nil, err
	}

	actual, actualErr := s.normalizedActual()

	state := &State{
		Rendered: string(rendered),
		Actual:   string(actual),
		Changed:  !bytes.Equal(rendered, actual),
		Warnings: warnings,
	}

	if actualErr != nil {
		state.ActualError = actualErr.Error()
	}

	return state, nil
}

// Changed проверяет, отличается ли отрендеренный конфиг от рабочего.
func (s *Service) Changed() (bool, error) {
	state, err := s.State()
	if err != nil {
		return false, err
	}

	return state.Changed, nil
}

// Apply проверяет отрендеренный конфиг, атомарно записывает его в рабочий и перезапускает sing-box.
// Если sing-box не запустился, восстанавливается прежний конфиг.
func (s *Service) Apply(ctx context.Context) (*ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rendered, warnings, err := s.Render()
	if err != nil {
		return nil, err
	}

	if actual, _ := s.normalizedActual(); bytes.Equal(rendered, actual) {
		return nil, ErrNoChanges
	}

	container, err := s.findContainer(ctx)
	if err != nil {
		return nil, err
	}

	if err = s.check(ctx, container.ID, rendered); err != nil {
		return nil, err
	}

	// Проверяем доступность Clash API до перезапуска: по нему потом определяется, что sing-box поднялся.
	_, clashErr := s.clash.GetVersion()
	clashReachable := clashErr == nil

	backup, err := s.provider.Backup()
	if err != nil {
		return nil, err
	}

	if err = s.provider.Write(rendered); err != nil {
		return nil, err
	}

	log.Printf("sing-box config written (backup: %s), restarting sing-box", backup)

	startErr := s.restart(ctx, container.ID, clashReachable)
	if startErr == nil {
		return &ApplyResult{
			Message:  "Конфиг применен, sing-box перезапущен",
			Warnings: warnings,
			Backup:   backup,
		}, nil
	}

	logs, _ := s.controller.ContainerLogs(ctx, container.ID, logsTail)

	if backup == "" {
		return nil, fmt.Errorf("sing-box не запустился с новым конфигом, прежнего конфига нет: %w\n%s", startErr, logs)
	}

	log.Printf("sing-box failed to start with new config, restoring %s: %v", backup, startErr)

	if err = s.provider.Restore(backup); err != nil {
		return nil, fmt.Errorf("sing-box не запустился с новым конфигом (%v), восстановить прежний не удалось: %w", startErr, err)
	}

	if err = s.restart(ctx, container.ID, clashReachable); err != nil {
		return nil, fmt.Errorf("sing-box не запустился с новым конфигом (%v), прежний конфиг восстановлен, но sing-box не запустился и с ним: %w", startErr, err)
	}

	return nil, fmt.Errorf("sing-box не запустился с новым конфигом, прежний конфиг восстановлен: %w\n%s", startErr, logs)
}

// Restart перезапускает sing-box без изменения конфига. Не выполняется одновременно с применением конфига.
func (s *Service) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.controller.RestartContainersByLabels(containerLabels)
}

// normalizedActual возвращает рабочий конфиг в том же виде, что и рендер. Если конфиг не разбирается,
// возвращается его содержимое как есть и ошибка.
func (s *Service) normalizedActual() ([]byte, error) {
	data, err := s.provider.GetActualConfig()
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, errors.New("рабочего конфига sing-box еще нет")
	}

	if err != nil {
		return []byte{}, err
	}

	config, err := singboxconfig.Parse(data)
	if err != nil {
		return data, err
	}

	normalized, err := singboxconfig.Marshal(config)
	if err != nil {
		return data, err
	}

	return normalized, nil
}

// findContainer находит запущенный контейнер sing-box.
func (s *Service) findContainer(ctx context.Context) (*dockercontroller.Container, error) {
	containers, err := s.controller.ListContainers(ctx, dockercontroller.ContainerFilter{
		ID:     "",
		Name:   "",
		Labels: containerLabels,
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось найти контейнер sing-box: %w", err)
	}

	for i := range containers {
		if containers[i].Running && !containers[i].Restarting {
			return &containers[i], nil
		}
	}

	if len(containers) == 0 {
		return nil, errors.New("контейнер sing-box с лейблами app=sing-box и managed=true не найден")
	}

	return nil, fmt.Errorf("контейнер sing-box %s не запущен: проверить конфиг негде", containers[0].Name)
}

// check проверяет конфиг командой sing-box check внутри контейнера sing-box.
func (s *Service) check(ctx context.Context, containerID string, config []byte) error {
	result, err := s.controller.Exec(ctx, containerID, checkCommand, config)
	if errors.Is(err, dockercontroller.ErrNotFound) {
		return fmt.Errorf("docker-controller не поддерживает exec (нужна версия v0.0.3+): %w", err)
	}

	if err != nil {
		return fmt.Errorf("не удалось выполнить sing-box check: %w", err)
	}

	if result.ExitCode != 0 {
		output := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)

		return fmt.Errorf("%w: %s", ErrCheckFailed, output)
	}

	return nil
}

// restart перезапускает sing-box и ждет его запуска. Если Clash API был доступен до перезапуска,
// запуск подтверждается ответом Clash API, иначе — тем, что контейнер работает без перезапусков.
func (s *Service) restart(ctx context.Context, containerID string, clashReachable bool) error {
	if err := s.controller.RestartContainersByLabels(containerLabels); err != nil {
		return fmt.Errorf("не удалось перезапустить sing-box: %w", err)
	}

	deadline := time.Now().Add(startTimeout)

	var runningSince time.Time

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-time.After(time.Second):
		}

		container, err := s.controller.GetContainer(ctx, containerID)
		if err != nil {
			continue
		}

		if container.Exited() {
			return fmt.Errorf("sing-box завершился с кодом %d", container.ExitCode)
		}

		if !container.Running {
			runningSince = time.Time{}

			continue
		}

		if runningSince.IsZero() {
			runningSince = time.Now()
		}

		if clashReachable {
			if _, err = s.clash.GetVersion(); err == nil {
				return nil
			}

			continue
		}

		if time.Since(runningSince) >= stableRunTime {
			return nil
		}
	}

	return fmt.Errorf("sing-box не запустился за %s", startTimeout)
}
