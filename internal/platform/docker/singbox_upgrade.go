package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/dockerapi"
	"github.com/lanfix/sing-box-configurer/internal/updater"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

// Ключи состояния обновления sing-box в журнале.
const (
	stateSingBoxName     = "sing_box_container"
	stateSingBoxOldID    = "sing_box_old_id"
	stateSingBoxNewImage = "sing_box_new_image"
	stateSingBoxRollback = "sing_box_rollback_name"
	stateSingBoxReplaced = "sing_box_replaced"
)

const (
	// singBoxStableTime — сколько новый sing-box должен проработать без перезапусков.
	singBoxStableTime = 10 * time.Second

	// singBoxPollInterval — как часто проверяется состояние нового sing-box.
	singBoxPollInterval = 2 * time.Second
)

// prepareSingBox решает, нужно ли обновить контейнер sing-box: только образ lanfix/sing-box-lx
// старше version.SingBoxVersion. Свой образ или более новая версия не трогаются.
func (t *Target) prepareSingBox(ctx context.Context, journal *updater.Journal) {
	containers, err := t.docker.List(ctx, dockerapi.Filter{
		ID:     "",
		Name:   "",
		Labels: singBoxLabels,
	})
	if err != nil || len(containers) != 1 {
		t.log.Warn("sing-box container not found, skipping sing-box update", "step", updater.StepPrepare, "found", len(containers), "error", fmt.Sprint(err))

		return
	}

	info := containers[0]
	repository, tag := splitImage(info.Image)

	if strings.TrimPrefix(repository, "docker.io/") != version.SingBoxRepository {
		t.log.Info("sing-box uses a custom image, skipping sing-box update", "step", updater.StepPrepare, "image", info.Image)

		return
	}

	if cmp, ok := version.CompareSingBox(tag, version.SingBoxVersion); !ok || cmp >= 0 {
		t.log.Info("sing-box is up to date", "step", updater.StepPrepare, "image", info.Image, "required", version.SingBoxVersion)

		return
	}

	journal.State[stateSingBoxName] = info.Name
	journal.State[stateSingBoxOldID] = info.ID
	journal.State[stateSingBoxNewImage] = repository + ":" + version.SingBoxVersion
	journal.State[stateSingBoxRollback] = fmt.Sprintf("%s-rollback-%s", info.Name, journal.ID)
}

// downloadSingBox загружает образ нового sing-box заранее: пока sing-box остановлен, хост может остаться без DNS.
func (t *Target) downloadSingBox(ctx context.Context, journal *updater.Journal) error {
	image := journal.State[stateSingBoxNewImage]
	if image == "" {
		return nil
	}

	t.log.Info("pulling image "+image, "step", updater.StepDownload)

	return t.docker.Pull(ctx, image)
}

// upgradeSingBox пересоздает контейнер sing-box из нового образа после проверки новой версии конфигуратора.
// Если новый sing-box не запустился, возвращается прежний контейнер, а обновление конфигуратора не откатывается:
// он работает и с прежним sing-box.
func (t *Target) upgradeSingBox(ctx context.Context, journal *updater.Journal) error {
	image := journal.State[stateSingBoxNewImage]
	if image == "" {
		return nil
	}

	name, oldID, rollbackName := journal.State[stateSingBoxName], journal.State[stateSingBoxOldID], journal.State[stateSingBoxRollback]

	t.log.Info("updating sing-box to "+image, "step", updater.StepFinish, "container", name)

	result, err := t.docker.Replace(ctx, name, image, rollbackName)
	if err == nil {
		err = t.waitSingBox(ctx, result.NewID)
	}

	if err != nil {
		t.log.Warn("sing-box update failed, restoring previous container", "step", updater.StepFinish, "error", err.Error())

		if restoreErr := t.docker.Restore(ctx, name, oldID, rollbackName); restoreErr != nil {
			return fmt.Errorf("cannot restore sing-box after failed update: %w", restoreErr)
		}

		return t.docker.Start(ctx, name)
	}

	journal.State[stateSingBoxReplaced] = "true"

	return t.setComposeTag(ctx, journal, image)
}

// waitSingBox ждет, пока новый sing-box проработает singBoxStableTime без падений и перезапусков.
func (t *Target) waitSingBox(ctx context.Context, id string) error {
	deadline := time.Now().Add(singBoxStableTime)

	for {
		info, err := t.docker.Inspect(ctx, id)
		if err != nil {
			return err
		}

		if info.Exited() || info.Restarting {
			logs, _ := t.docker.Logs(ctx, id, "20")

			return fmt.Errorf("new sing-box exited with code %d:\n%s", info.ExitCode, strings.TrimSpace(logs))
		}

		if time.Now().After(deadline) {
			return nil
		}

		if err = sleep(ctx, singBoxPollInterval); err != nil {
			return err
		}
	}
}

// commitSingBox удаляет прежний контейнер sing-box, сохраненный для отката.
func (t *Target) commitSingBox(ctx context.Context, journal *updater.Journal) error {
	if journal.State[stateSingBoxReplaced] != "true" {
		return nil
	}

	name, rollbackName := journal.State[stateSingBoxName], journal.State[stateSingBoxRollback]

	if err := dockerapi.ValidateRollbackName(name, rollbackName); err != nil {
		return err
	}

	return t.docker.Remove(ctx, rollbackName)
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
