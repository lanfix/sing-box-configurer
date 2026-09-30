// Package dockerapi содержит операции над контейнерами через Docker API (сокет docker.sock), из которых
// строятся управление sing-box и обновление конфигуратора: exec, перезапуск, запуск job-контейнеров,
// замена образа с сохранением настроек и возврат исходного контейнера.
package dockerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
)

const (
	// composeImageLabel — лейбл compose с digest образа, при смене образа он становится неверным.
	composeImageLabel = "com.docker.compose.image"

	stopTimeoutSeconds = 15

	// execOutputLimit ограничивает объем вывода команды, выполняемой через Exec.
	execOutputLimit = 8 << 20

	// logsLimit ограничивает объем логов, читаемых через Logs.
	logsLimit = 8 << 20
)

// ErrNotFound возвращается, если контейнер не найден.
var ErrNotFound = errors.New("container not found")

// rollbackNamePattern — имя контейнера, сохраненного для отката: <name>-rollback-<id>.
var rollbackNamePattern = regexp.MustCompile(`^(.+)-rollback-[0-9A-Za-z_-]+$`)

// Mount описывает точку монтирования контейнера.
type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// Info описывает контейнер.
type Info struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	Running    bool              `json:"running"`
	Restarting bool              `json:"restarting"`
	ExitCode   int               `json:"exit_code"`
	Error      string            `json:"error,omitempty"`
	StartedAt  string            `json:"started_at"`
	FinishedAt string            `json:"finished_at"`
	Labels     map[string]string `json:"labels"`
	Networks   []string          `json:"networks"`
	Mounts     []Mount           `json:"mounts"`
}

// Exited возвращает true, если контейнер завершился или ушел в цикл перезапусков.
func (i Info) Exited() bool {
	return i.Restarting || i.State == string(container.StateExited) || i.State == string(container.StateDead)
}

// Filter — фильтры списка контейнеров. Пустые поля не учитываются.
type Filter struct {
	// ID — ID контейнера или его префикс (контейнер находит сам себя по hostname).
	ID     string
	Name   string
	Labels map[string]string
}

// RunRequest описывает запуск контейнера.
type RunRequest struct {
	Image         string
	Name          string
	Cmd           []string
	Env           []string
	Labels        map[string]string
	Binds         []string
	Network       string
	RestartPolicy container.RestartPolicy

	// Pull — загрузить образ, даже если он уже есть локально.
	Pull bool
}

// ReplaceResult — результат замены контейнера.
type ReplaceResult struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}

// ExecResult — результат выполнения команды в контейнере.
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// Manager выполняет операции над контейнерами через Docker API.
type Manager struct {
	client *client.Client
}

// NewManager создает менеджер с клиентом Docker из переменных окружения (DOCKER_HOST и др.,
// по умолчанию unix:///var/run/docker.sock).
func NewManager() (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("cannot create docker client: %w", err)
	}

	return &Manager{
		client: cli,
	}, nil
}

// Ping проверяет доступность Docker API.
func (m *Manager) Ping(ctx context.Context) error {
	if _, err := m.client.Ping(ctx); err != nil {
		return fmt.Errorf("docker api is unavailable (is /var/run/docker.sock mounted?): %w", err)
	}

	return nil
}

// Inspect возвращает информацию о контейнере по имени или ID.
func (m *Manager) Inspect(ctx context.Context, nameOrID string) (Info, error) {
	info, err := m.client.ContainerInspect(ctx, nameOrID)
	if errdefs.IsNotFound(err) {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, nameOrID)
	}

	if err != nil {
		return Info{}, fmt.Errorf("cannot inspect container %s: %w", nameOrID, err)
	}

	return toInfo(info), nil
}

// List возвращает контейнеры (включая остановленные), подходящие под все фильтры.
func (m *Manager) List(ctx context.Context, filter Filter) ([]Info, error) {
	args := filters.NewArgs()

	for key, value := range filter.Labels {
		args.Add("label", key+"="+value)
	}

	if filter.Name != "" {
		args.Add("name", filter.Name)
	}

	if filter.ID != "" {
		args.Add("id", filter.ID)
	}

	list, err := m.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list containers: %w", err)
	}

	result := make([]Info, 0, len(list))

	for _, item := range list {
		info, err := m.Inspect(ctx, item.ID)
		if err != nil {
			// Контейнер могли удалить между list и inspect.
			continue
		}

		result = append(result, info)
	}

	return result, nil
}

// Pull загружает образ и проверяет ошибки в потоке прогресса.
func (m *Manager) Pull(ctx context.Context, ref string) error {
	reader, err := m.client.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("cannot pull %s: %w", ref, err)
	}

	defer func() {
		_ = reader.Close()
	}()

	decoder := json.NewDecoder(reader)

	for {
		var message struct {
			Error string `json:"error"`
		}

		if err = decoder.Decode(&message); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("cannot read pull progress of %s: %w", ref, err)
		}

		if message.Error != "" {
			return fmt.Errorf("cannot pull %s: %s", ref, message.Error)
		}
	}
}

// EnsureImage загружает образ, если его нет локально или если pull=true.
func (m *Manager) EnsureImage(ctx context.Context, ref string, pull bool) error {
	if !pull {
		if _, err := m.client.ImageInspect(ctx, ref); err == nil {
			return nil
		}
	}

	return m.Pull(ctx, ref)
}

// Run загружает образ, создает и запускает контейнер.
func (m *Manager) Run(ctx context.Context, req RunRequest) (Info, error) {
	if err := m.EnsureImage(ctx, req.Image, req.Pull); err != nil {
		return Info{}, err
	}

	var networking *network.NetworkingConfig

	if req.Network != "" {
		networking = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				req.Network: {},
			},
		}
	}

	created, err := m.client.ContainerCreate(ctx, &container.Config{
		Image:  req.Image,
		Cmd:    req.Cmd,
		Env:    req.Env,
		Labels: req.Labels,
	}, &container.HostConfig{
		Binds:         req.Binds,
		RestartPolicy: req.RestartPolicy,
	}, networking, nil, req.Name)
	if err != nil {
		return Info{}, fmt.Errorf("cannot create container %s: %w", req.Name, err)
	}

	if err = m.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = m.Remove(context.WithoutCancel(ctx), created.ID)

		return Info{}, fmt.Errorf("cannot start container %s: %w", req.Name, err)
	}

	return m.Inspect(ctx, created.ID)
}

// Restart перезапускает контейнер.
func (m *Manager) Restart(ctx context.Context, nameOrID string) error {
	timeout := stopTimeoutSeconds

	if err := m.client.ContainerRestart(ctx, nameOrID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("cannot restart %s: %w", nameOrID, err)
	}

	return nil
}

// Replace останавливает контейнер name, переименовывает его в rollbackName и создает на его месте
// новый контейнер с образом newImage и теми же настройками (тома, порты, сети, лейблы, restart policy).
// Значения, унаследованные от старого образа (команда, env, лейблы версии), не переносятся.
// Образ должен быть загружен заранее.
func (m *Manager) Replace(ctx context.Context, name, newImage, rollbackName string) (ReplaceResult, error) {
	if err := ValidateRollbackName(name, rollbackName); err != nil {
		return ReplaceResult{}, err
	}

	old, err := m.client.ContainerInspect(ctx, name)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("cannot inspect %s: %w", name, err)
	}

	oldImage, err := m.client.ImageInspect(ctx, old.Image)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("cannot inspect old image: %w", err)
	}

	if _, err = m.client.ImageInspect(ctx, newImage); err != nil {
		return ReplaceResult{}, fmt.Errorf("image %s is not pulled: %w", newImage, err)
	}

	config := CleanInheritedConfig(old, oldImage)
	config.Image = newImage

	result := ReplaceResult{
		OldID: old.ID,
		NewID: "",
	}

	timeout := stopTimeoutSeconds

	if err = m.client.ContainerStop(ctx, old.ID, container.StopOptions{Timeout: &timeout}); err != nil {
		return result, fmt.Errorf("cannot stop %s: %w", name, err)
	}

	if err = m.client.ContainerRename(ctx, old.ID, rollbackName); err != nil {
		return result, fmt.Errorf("cannot rename %s: %w", name, err)
	}

	created, err := m.client.ContainerCreate(ctx, config, old.HostConfig, EndpointsFrom(old), nil, name)
	if err != nil {
		return result, fmt.Errorf("cannot create new %s: %w", name, err)
	}

	result.NewID = created.ID

	if err = m.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return result, fmt.Errorf("cannot start new %s: %w", name, err)
	}

	return result, nil
}

// Restore возвращает исходный контейнер: удаляет контейнер name, если это не oldID, и возвращает
// имя контейнеру rollbackName. Старый контейнер не запускается — это делает вызывающий (после
// восстановления файлов). Операция идемпотентна.
func (m *Manager) Restore(ctx context.Context, name, oldID, rollbackName string) error {
	if err := ValidateRollbackName(name, rollbackName); err != nil {
		return err
	}

	current, err := m.client.ContainerInspect(ctx, name)

	switch {
	case errdefs.IsNotFound(err):

	case err != nil:
		return fmt.Errorf("cannot inspect %s: %w", name, err)

	case current.ID != oldID:
		if err = m.client.ContainerRemove(ctx, current.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("cannot remove new %s: %w", name, err)
		}
	}

	rollback, err := m.client.ContainerInspect(ctx, rollbackName)

	switch {
	case errdefs.IsNotFound(err):
		return nil

	case err != nil:
		return fmt.Errorf("cannot inspect %s: %w", rollbackName, err)
	}

	if oldID != "" && rollback.ID != oldID {
		return fmt.Errorf("container %s is not the original container %s", rollbackName, oldID)
	}

	if err = m.client.ContainerRename(ctx, rollback.ID, name); err != nil {
		return fmt.Errorf("cannot rename %s back: %w", rollbackName, err)
	}

	return nil
}

// Start запускает контейнер, если он не запущен.
func (m *Manager) Start(ctx context.Context, nameOrID string) error {
	info, err := m.client.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return fmt.Errorf("cannot inspect %s: %w", nameOrID, err)
	}

	if info.State != nil && info.State.Running {
		return nil
	}

	if err = m.client.ContainerStart(ctx, info.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("cannot start %s: %w", nameOrID, err)
	}

	return nil
}

// Remove удаляет контейнер, если он существует.
func (m *Manager) Remove(ctx context.Context, nameOrID string) error {
	err := m.client.ContainerRemove(ctx, nameOrID, container.RemoveOptions{Force: true})
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("cannot remove %s: %w", nameOrID, err)
	}

	return nil
}

// Logs возвращает последние tail строк логов контейнера (stdout и stderr). tail = "all" — все строки.
func (m *Manager) Logs(ctx context.Context, nameOrID, tail string) (string, error) {
	reader, err := m.client.ContainerLogs(ctx, nameOrID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if errdefs.IsNotFound(err) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, nameOrID)
	}

	if err != nil {
		return "", fmt.Errorf("cannot get logs of %s: %w", nameOrID, err)
	}

	defer func() {
		_ = reader.Close()
	}()

	var output bytes.Buffer

	// Контейнеры без TTY отдают мультиплексированный поток stdout/stderr.
	if _, err = stdcopy.StdCopy(&output, &output, io.LimitReader(reader, logsLimit)); err != nil {
		return "", fmt.Errorf("cannot read logs: %w", err)
	}

	return output.String(), nil
}

// Exec выполняет команду cmd в запущенном контейнере. Если stdin не nil, он передается на стандартный
// ввод команды. Вывод stdout и stderr возвращается раздельно вместе с кодом выхода.
func (m *Manager) Exec(ctx context.Context, nameOrID string, cmd []string, stdin []byte) (ExecResult, error) {
	created, err := m.client.ContainerExecCreate(ctx, nameOrID, container.ExecOptions{
		AttachStdin:  stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if errdefs.IsNotFound(err) {
		return ExecResult{}, fmt.Errorf("%w: %s", ErrNotFound, nameOrID)
	}

	if err != nil {
		return ExecResult{}, fmt.Errorf("cannot create exec in %s: %w", nameOrID, err)
	}

	attached, err := m.client.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("cannot attach to exec in %s: %w", nameOrID, err)
	}

	defer attached.Close()

	// stdin пишется параллельно чтению: команда может заполнить буфер вывода раньше, чем прочитает ввод.
	if stdin != nil {
		go func() {
			_, _ = attached.Conn.Write(stdin)
			_ = attached.CloseWrite()
		}()
	}

	var stdout, stderr bytes.Buffer

	// Без TTY вывод мультиплексирован на stdout/stderr.
	if _, err = stdcopy.StdCopy(&stdout, &stderr, io.LimitReader(attached.Reader, execOutputLimit)); err != nil {
		return ExecResult{}, fmt.Errorf("cannot read exec output: %w", err)
	}

	inspect, err := m.client.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return ExecResult{}, fmt.Errorf("cannot inspect exec in %s: %w", nameOrID, err)
	}

	// Вывод оборвался по лимиту, а команда еще работает: код выхода неизвестен.
	if inspect.Running {
		return ExecResult{}, fmt.Errorf("exec output in %s exceeds %d bytes", nameOrID, execOutputLimit)
	}

	return ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// ValidateRollbackName проверяет, что rollbackName имеет вид <name>-rollback-<id>.
// Это не дает переименовывать и удалять произвольные контейнеры при откате.
func ValidateRollbackName(name, rollbackName string) error {
	match := rollbackNamePattern.FindStringSubmatch(rollbackName)
	if match == nil || match[1] != name {
		return fmt.Errorf("invalid rollback name %q for container %q", rollbackName, name)
	}

	return nil
}

// CleanInheritedConfig возвращает конфиг контейнера без значений, унаследованных от старого образа,
// чтобы новый контейнер получил значения по умолчанию из нового образа (команду, env, лейблы версии).
func CleanInheritedConfig(old container.InspectResponse, oldImage image.InspectResponse) *container.Config {
	config := *old.Config

	var (
		imageEnv        []string
		imageCmd        []string
		imageEntrypoint []string
		imageWorkingDir string
		imageLabels     map[string]string
	)

	if oldImage.Config != nil {
		imageEnv = oldImage.Config.Env
		imageCmd = oldImage.Config.Cmd
		imageEntrypoint = oldImage.Config.Entrypoint
		imageWorkingDir = oldImage.Config.WorkingDir
		imageLabels = oldImage.Config.Labels
	}

	config.Env = slices.DeleteFunc(slices.Clone(config.Env), func(env string) bool {
		return slices.Contains(imageEnv, env)
	})

	if slices.Equal(config.Cmd, imageCmd) {
		config.Cmd = nil
	}

	if slices.Equal(config.Entrypoint, imageEntrypoint) {
		config.Entrypoint = nil
	}

	if config.WorkingDir == imageWorkingDir {
		config.WorkingDir = ""
	}

	labels := make(map[string]string, len(config.Labels))

	for key, value := range config.Labels {
		if imageValue, ok := imageLabels[key]; ok && imageValue == value {
			continue
		}

		if key == composeImageLabel {
			continue
		}

		labels[key] = value
	}

	config.Labels = labels

	// Hostname по умолчанию равен короткому ID контейнера — новый контейнер получит свой.
	if config.Hostname != "" && strings.HasPrefix(old.ID, config.Hostname) {
		config.Hostname = ""
	}

	return &config
}

// EndpointsFrom возвращает настройки сетей контейнера для создания его копии.
func EndpointsFrom(old container.InspectResponse) *network.NetworkingConfig {
	endpoints := map[string]*network.EndpointSettings{}

	if old.NetworkSettings == nil {
		return &network.NetworkingConfig{
			EndpointsConfig: endpoints,
		}
	}

	shortID := old.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	for name, settings := range old.NetworkSettings.Networks {
		if settings == nil {
			continue
		}

		// Alias с коротким ID старого контейнера Docker добавляет сам, переносить его нельзя.
		aliases := slices.DeleteFunc(slices.Clone(settings.Aliases), func(alias string) bool {
			return alias == shortID
		})

		endpoints[name] = &network.EndpointSettings{
			IPAMConfig: settings.IPAMConfig,
			Links:      settings.Links,
			Aliases:    aliases,
			DriverOpts: settings.DriverOpts,
		}
	}

	return &network.NetworkingConfig{
		EndpointsConfig: endpoints,
	}
}

// toInfo конвертирует ответ Docker API в Info.
func toInfo(info container.InspectResponse) Info {
	result := Info{
		ID:         info.ID,
		Name:       strings.TrimPrefix(info.Name, "/"),
		Image:      "",
		State:      "",
		Running:    false,
		Restarting: false,
		ExitCode:   0,
		Error:      "",
		StartedAt:  "",
		FinishedAt: "",
		Labels:     map[string]string{},
		Networks:   []string{},
		Mounts:     []Mount{},
	}

	if info.Config != nil {
		result.Image = info.Config.Image
		result.Labels = info.Config.Labels
	}

	if info.State != nil {
		result.State = string(info.State.Status)
		result.Running = info.State.Running
		result.Restarting = info.State.Restarting
		result.ExitCode = info.State.ExitCode
		result.Error = info.State.Error
		result.StartedAt = info.State.StartedAt
		result.FinishedAt = info.State.FinishedAt
	}

	if info.NetworkSettings != nil {
		for name := range info.NetworkSettings.Networks {
			result.Networks = append(result.Networks, name)
		}

		slices.Sort(result.Networks)
	}

	for _, mount := range info.Mounts {
		result.Mounts = append(result.Mounts, Mount{
			Type:        string(mount.Type),
			Source:      mount.Source,
			Destination: mount.Destination,
		})
	}

	return result
}
