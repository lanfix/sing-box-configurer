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
	"path/filepath"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

const (
	// Сколько ждать запуска sing-box после перезапуска.
	startTimeout = 30 * time.Second

	// Сколько sing-box должен проработать, чтобы считаться запущенным, если Clash API недоступен.
	stableRunTime = 5 * time.Second

	// Сколько строк логов sing-box показывать при неудачном запуске.
	logsTail = 30
)

var (
	// ErrNoChanges — отрендеренный конфиг совпадает с рабочим.
	ErrNoChanges = errors.New("конфиг не изменился")

	// ErrCheckFailed — sing-box check отклонил конфиг.
	ErrCheckFailed = errors.New("sing-box check отклонил конфиг")
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
	source   InputSource
	provider *singboxconfig.Provider
	runtime  platform.SingBox
	clash    *singboxclashapi.ClashAPI

	// Применения выполняются по одному.
	mu sync.Mutex
}

// NewService создает сервис. runtime управляет процессом sing-box на платформе установки.
func NewService(source InputSource, provider *singboxconfig.Provider, runtime platform.SingBox, clash *singboxclashapi.ClashAPI) *Service {
	return &Service{
		source:   source,
		provider: provider,
		runtime:  runtime,
		clash:    clash,
		mu:       sync.Mutex{},
	}
}

// EnsureConfig записывает отрендеренный конфиг, если рабочего конфига еще нет (новая инсталляция):
// sing-box сможет запуститься, а применять следующие конфиги можно будет с проверкой sing-box check.
func (s *Service) EnsureConfig() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.provider.GetActualConfig(); !errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	rendered, _, err := s.Render()
	if err != nil {
		return false, err
	}

	if err = os.MkdirAll(filepath.Dir(s.provider.Path()), 0755); err != nil {
		return false, fmt.Errorf("cannot create sing-box config dir: %w", err)
	}

	if err = s.provider.Write(rendered); err != nil {
		return false, err
	}

	return true, nil
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

	return s.applyLocked(ctx, rendered, warnings)
}

// applyLocked проверяет конфиг data, атомарно записывает его в рабочий и перезапускает sing-box. Если sing-box
// не запустился, восстанавливается прежний конфиг. Вызывается под блокировкой s.mu.
func (s *Service) applyLocked(ctx context.Context, data []byte, warnings []string) (*ApplyResult, error) {
	if err := s.check(ctx, data); err != nil {
		return nil, err
	}

	// Проверяем доступность Clash API до перезапуска: по нему потом определяется, что sing-box поднялся.
	_, clashErr := s.clash.GetVersion()
	clashReachable := clashErr == nil

	backup, err := s.provider.Backup()
	if err != nil {
		return nil, err
	}

	if err = s.provider.Write(data); err != nil {
		return nil, err
	}

	log.Printf("sing-box config written (backup: %s), restarting sing-box", backup)

	startErr := s.restart(ctx, clashReachable)
	if startErr == nil {
		return &ApplyResult{
			Message:  "Конфиг применен, sing-box перезапущен",
			Warnings: warnings,
			Backup:   backup,
		}, nil
	}

	logs, _ := s.runtime.Logs(ctx, logsTail)

	if backup == "" {
		return nil, fmt.Errorf("sing-box не запустился с новым конфигом, прежнего конфига нет: %w\n%s", startErr, logs)
	}

	log.Printf("sing-box failed to start with new config, restoring %s: %v", backup, startErr)

	if err = s.provider.Restore(backup); err != nil {
		return nil, fmt.Errorf("sing-box не запустился с новым конфигом (%v), восстановить прежний не удалось: %w", startErr, err)
	}

	if err = s.restart(ctx, clashReachable); err != nil {
		return nil, fmt.Errorf("sing-box не запустился с новым конфигом (%v), прежний конфиг восстановлен, но sing-box не запустился и с ним: %w", startErr, err)
	}

	return nil, fmt.Errorf("sing-box не запустился с новым конфигом, прежний конфиг восстановлен: %w\n%s", startErr, logs)
}

// Backups возвращает резервные копии рабочего конфига, новые первыми.
func (s *Service) Backups() ([]singboxconfig.BackupInfo, error) {
	return s.provider.Backups()
}

// Backup возвращает резервную копию name в том же виде, что и рендер (для сравнения с рабочим конфигом).
func (s *Service) Backup(name string) ([]byte, error) {
	data, err := s.provider.ReadBackup(name)
	if err != nil {
		return nil, err
	}

	return normalize(data)
}

// RestoreBackup делает рабочим конфиг из резервной копии name так же, как применение: проверка sing-box check,
// резервная копия текущего конфига, замена и перезапуск, а при неудачном запуске — возврат текущего конфига.
// Данные app.json не меняются, поэтому после отката итоговый конфиг отличается от рабочего.
func (s *Service) RestoreBackup(ctx context.Context, name string) (*ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.provider.ReadBackup(name)
	if err != nil {
		return nil, err
	}

	backup, err := normalize(data)
	if err != nil {
		return nil, fmt.Errorf("резервная копия %s повреждена: %w", name, err)
	}

	if actual, _ := s.normalizedActual(); bytes.Equal(backup, actual) {
		return nil, ErrNoChanges
	}

	result, err := s.applyLocked(ctx, backup, nil)
	if err != nil {
		return nil, err
	}

	result.Message = "Конфиг из резервной копии применен, sing-box перезапущен"

	return result, nil
}

// Restart перезапускает sing-box без изменения конфига. Не выполняется одновременно с применением конфига.
func (s *Service) Restart(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.runtime.Restart(ctx)
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

	normalized, err := normalize(data)
	if err != nil {
		return data, err
	}

	return normalized, nil
}

// normalize приводит конфиг к виду рендера: без комментариев, с отсортированными ключами и отступами.
func normalize(data []byte) ([]byte, error) {
	config, err := singboxconfig.Parse(data)
	if err != nil {
		return nil, err
	}

	return singboxconfig.Marshal(config)
}

// check проверяет конфиг командой sing-box check.
func (s *Service) check(ctx context.Context, config []byte) error {
	result, err := s.runtime.Check(ctx, config)
	if err != nil {
		return fmt.Errorf("не удалось выполнить sing-box check: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("%w: %s", ErrCheckFailed, result.Output)
	}

	return nil
}

// restart перезапускает sing-box и ждет его запуска. Если Clash API был доступен до перезапуска,
// запуск подтверждается ответом Clash API, иначе — тем, что sing-box работает без перезапусков.
func (s *Service) restart(ctx context.Context, clashReachable bool) error {
	if err := s.runtime.Restart(ctx); err != nil {
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

		state, err := s.runtime.State(ctx)
		if err != nil {
			continue
		}

		if state.Failed {
			return fmt.Errorf("sing-box завершился с кодом %d", state.ExitCode)
		}

		if !state.Running {
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
