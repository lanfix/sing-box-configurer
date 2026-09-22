package updater

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/semver"
)

const (
	pollInterval         = 2 * time.Second
	failureLogTailLines  = "40"
	maxFailureLogsLength = 4000
)

// updateController обновляет docker-controller, если его версия ниже ControllerVersion.
// Контроллер обновляет сам себя (job из нового образа) и сам откатывается при ошибке,
// поэтому updater только запускает self-update и ждет его результата.
func (u *Updater) updateController(ctx context.Context) error {
	if u.opts.ControllerContainer == "" {
		return nil
	}

	current, err := u.controller.Version(ctx)
	if errors.Is(err, dockercontroller.ErrNotFound) {
		return fmt.Errorf("docker-controller does not support updates, update it manually to %s or newer", ControllerVersion)
	}

	if err != nil {
		return err
	}

	if !semver.IsValid(current) {
		u.log.Warn("docker-controller is not a release build, skipping its update", "step", stepPrepare, "version", current)

		return nil
	}

	if semver.Compare(current, ControllerVersion) >= 0 {
		return nil
	}

	info, err := u.controller.GetContainer(ctx, u.opts.ControllerContainer)
	if err != nil {
		return err
	}

	repository, _ := splitImage(info.Image)
	image := repository + ":" + ControllerVersion

	u.setStep(stepController, fmt.Sprintf("updating docker-controller %s -> %s", current, ControllerVersion))

	job, err := u.controller.SelfUpdate(ctx, image)
	if err != nil {
		return err
	}

	if err = u.waitControllerJob(ctx, job.Name); err != nil {
		return err
	}

	if current, err = u.controller.Version(ctx); err != nil {
		return err
	}

	if current != ControllerVersion {
		return fmt.Errorf("docker-controller reports version %s after update, want %s", current, ControllerVersion)
	}

	u.log.Info("docker-controller updated", "step", stepController, "version", current)

	// Тег контроллера фиксируем сразу: контроллер при откате конфигуратора не откатывается,
	// и бэкап compose-файла снимается уже с новым тегом.
	return u.updateCompose(image)
}

// waitControllerJob ждет завершения job самообновления контроллера и разбирает его результат.
// Пока контроллер перезапускается, его API недоступен — ошибки запросов в это время ожидаемы.
func (u *Updater) waitControllerJob(ctx context.Context, jobName string) error {
	deadline := time.Now().Add(controllerUpdateTimeout)

	var lastErr error

	for time.Now().Before(deadline) {
		if err := sleep(ctx, pollInterval); err != nil {
			return err
		}

		job, err := u.controller.GetContainer(ctx, jobName)
		if err != nil {
			lastErr = err

			continue
		}

		if job.Running || job.Restarting {
			continue
		}

		logs, err := u.controller.ContainerLogs(ctx, job.ID, "all")
		if err != nil {
			lastErr = err

			continue
		}

		result, message := parseResult(logs)

		switch result {
		case "succeeded":
			return nil

		case "":
			return fmt.Errorf("self-update job exited with code %d without result:\n%s", job.ExitCode, lastLines(logs, 20))
		}

		return fmt.Errorf("self-update %s: %s", strings.ReplaceAll(result, "_", " "), message)
	}

	return fmt.Errorf("docker-controller self-update timeout after %s: %v", controllerUpdateTimeout, lastErr)
}

// waitHealthy ждет, пока check вернет nil. Если контейнер упал или ушел в цикл перезапусков,
// ожидание прерывается, а в ошибку добавляются последние строки его логов.
func (u *Updater) waitHealthy(ctx context.Context, id string, timeout time.Duration, check func(ctx context.Context) error) error {
	deadline := time.Now().Add(timeout)

	var lastErr error

	for time.Now().Before(deadline) {
		info, err := u.controller.GetContainer(ctx, id)
		if err != nil {
			return err
		}

		if info.Exited() {
			return fmt.Errorf("container exited with code %d%s", info.ExitCode, u.logsSuffix(ctx, id))
		}

		if lastErr = check(ctx); lastErr == nil {
			return nil
		}

		if err = sleep(ctx, pollInterval); err != nil {
			return err
		}
	}

	return fmt.Errorf("health check timeout after %s: %v%s", timeout, lastErr, u.logsSuffix(ctx, id))
}

// logsSuffix возвращает последние строки логов контейнера для текста ошибки.
func (u *Updater) logsSuffix(ctx context.Context, id string) string {
	logs, err := u.controller.ContainerLogs(ctx, id, failureLogTailLines)
	if err != nil {
		return ""
	}

	logs = strings.TrimSpace(logs)
	if logs == "" {
		return ""
	}

	if len(logs) > maxFailureLogsLength {
		logs = "..." + logs[len(logs)-maxFailureLogsLength:]
	}

	return "\ncontainer logs:\n" + logs
}

// parseResult ищет в JSON-логах job строку с полем result.
func parseResult(logs string) (string, string) {
	scanner := bufio.NewScanner(strings.NewReader(logs))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	var result, message string

	for scanner.Scan() {
		var line struct {
			Result string `json:"result"`
			Error  string `json:"error"`
		}

		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Result != "" {
			result, message = line.Result, line.Error
		}
	}

	return result, message
}

// getJSON выполняет GET-запрос и раскладывает JSON-ответ в v.
func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// sleep ждет d или отмены контекста.
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-time.After(d):
		return nil
	}
}

// lastLines возвращает последние n строк текста.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
