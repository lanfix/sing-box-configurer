// Package migrations применяет миграции данных приложения (app.json) и конфига sing-box при старте.
//
// Миграции только прямые (вперед). Откат выполняется восстановлением бэкапа, который updater
// снимает перед обновлением. Все миграции применяются в памяти, файлы записываются один раз
// после успеха всех миграций. Версия схемы хранится в поле schema_version файла app.json.
package migrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// schemaVersionKey — поле app.json с версией схемы данных.
const schemaVersionKey = "schema_version"

var (
	// ErrNewerSchema возвращается, если данные созданы более новой версией приложения.
	ErrNewerSchema = errors.New("data schema is newer than supported by this version")

	// ErrOldSchema возвращается, если данные старше версии, с которой начинается список миграций.
	ErrOldSchema = errors.New("data schema is too old: update to v0.9.0 first")
)

// Migration описывает одну миграцию.
type Migration struct {
	// Version — версия схемы после применения миграции. Версии идут подряд с baseVersion+1.
	Version int

	// Name — краткое описание для логов.
	Name string

	// Up применяет миграцию к состоянию в памяти.
	Up func(state *State) error
}

// SingBoxConfigStore читает и записывает основной конфиг sing-box.
type SingBoxConfigStore interface {
	GetActualConfigParsed() (map[string]any, error)
	WriteActualConfig(config map[string]any) error
}

// State — данные, доступные миграции.
type State struct {
	// AppData — поля верхнего уровня app.json.
	AppData map[string]json.RawMessage

	singBoxStore   SingBoxConfigStore
	singBoxConfig  map[string]any
	singBoxChanged bool
}

// SingBoxConfig возвращает конфиг sing-box для изменения. Если миграция его изменила,
// она должна вызвать MarkSingBoxConfigChanged, иначе изменения не будут записаны.
func (s *State) SingBoxConfig() (map[string]any, error) {
	if s.singBoxConfig != nil {
		return s.singBoxConfig, nil
	}

	config, err := s.singBoxStore.GetActualConfigParsed()
	if err != nil {
		return nil, fmt.Errorf("cannot read sing-box config: %w", err)
	}

	s.singBoxConfig = config

	return config, nil
}

// MarkSingBoxConfigChanged помечает конфиг sing-box как измененный.
func (s *State) MarkSingBoxConfigChanged() {
	s.singBoxChanged = true
}

// Result описывает результат выполнения миграций.
type Result struct {
	FromVersion int      `json:"from_version"`
	ToVersion   int      `json:"to_version"`
	Applied     []string `json:"applied"`
}

// LatestVersion возвращает версию схемы, которую поддерживает эта версия приложения.
func LatestVersion() int {
	return latestVersion(baseVersion, registry)
}

// BaseVersion возвращает самую старую версию схемы, которую эта версия приложения умеет мигрировать.
func BaseVersion() int {
	return baseVersion
}

// SchemaVersion возвращает версию схемы данных fields. Данные без schema_version созданы до появления
// миграций — это версия 0.
func SchemaVersion(fields map[string]json.RawMessage) (int, error) {
	current := 0

	if raw, ok := fields[schemaVersionKey]; ok {
		if err := json.Unmarshal(raw, &current); err != nil {
			return 0, fmt.Errorf("invalid %s: %w", schemaVersionKey, err)
		}
	}

	return current, nil
}

// Run применяет недостающие миграции из реестра.
func Run(appData *appdata.File, singBoxStore SingBoxConfigStore) (*Result, error) {
	return run(appData, singBoxStore, baseVersion, registry)
}

// MigrateData приводит данные app.json fields к актуальной версии схемы в памяти (например, при импорте).
// Ничего не записывает: изменения конфига sing-box, которые сделали миграции, отбрасываются. fields
// меняется на месте.
func MigrateData(fields map[string]json.RawMessage, singBoxStore SingBoxConfigStore) (*Result, error) {
	if err := validate(baseVersion, registry); err != nil {
		return nil, err
	}

	result, _, err := migrate(fields, singBoxStore, baseVersion, registry)

	return result, err
}

// run применяет миграции из списка list, который начинается с версии base+1.
func run(appData *appdata.File, singBoxStore SingBoxConfigStore, base int, list []Migration) (*Result, error) {
	if err := validate(base, list); err != nil {
		return nil, err
	}

	latest := latestVersion(base, list)

	fields, err := appData.ReadRaw()
	if err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read app data: %w", err)
	}

	// Новая инсталляция: менеджеры создадут данные сразу в актуальном формате.
	if errors.Is(err, appdata.ErrNotExist) {
		if err = writeSchemaVersion(appData, latest); err != nil {
			return nil, err
		}

		return &Result{
			FromVersion: latest,
			ToVersion:   latest,
			Applied:     []string{},
		}, nil
	}

	result, state, err := migrate(fields, singBoxStore, base, list)
	if err != nil {
		return nil, err
	}

	if state == nil {
		return result, nil
	}

	// Конфиг sing-box пишем первым: версия схемы в app.json — признак завершения миграций.
	if state.singBoxChanged {
		if err = singBoxStore.WriteActualConfig(state.singBoxConfig); err != nil {
			return nil, fmt.Errorf("cannot write sing-box config: %w", err)
		}
	}

	if err = appData.WriteRaw(state.AppData); err != nil {
		return nil, fmt.Errorf("cannot write app data: %w", err)
	}

	return result, nil
}

// migrate применяет к fields в памяти миграции из списка list, который начинается с версии base+1.
// Если данные уже актуальны, возвращает nil вместо состояния.
func migrate(fields map[string]json.RawMessage, singBoxStore SingBoxConfigStore, base int, list []Migration) (*Result, *State, error) {
	latest := latestVersion(base, list)

	current, err := SchemaVersion(fields)
	if err != nil {
		return nil, nil, err
	}

	if current > latest {
		return nil, nil, fmt.Errorf("%w: data version %d, supported %d", ErrNewerSchema, current, latest)
	}

	if current < base {
		return nil, nil, fmt.Errorf("%w: data version %d, minimal %d", ErrOldSchema, current, base)
	}

	result := &Result{
		FromVersion: current,
		ToVersion:   latest,
		Applied:     []string{},
	}

	if current == latest {
		return result, nil, nil
	}

	state := &State{
		AppData:        fields,
		singBoxStore:   singBoxStore,
		singBoxConfig:  nil,
		singBoxChanged: false,
	}

	for _, migration := range list {
		if migration.Version <= current {
			continue
		}

		log.Printf("Applying migration %d: %s", migration.Version, migration.Name)

		if err = migration.Up(state); err != nil {
			return nil, nil, fmt.Errorf("migration %d (%s) failed: %w", migration.Version, migration.Name, err)
		}

		result.Applied = append(result.Applied, fmt.Sprintf("%d: %s", migration.Version, migration.Name))
	}

	versionRaw, _ := json.Marshal(latest)
	state.AppData[schemaVersionKey] = versionRaw

	return result, state, nil
}

// writeSchemaVersion записывает только версию схемы.
func writeSchemaVersion(appData *appdata.File, version int) error {
	fields := map[string]int{
		schemaVersionKey: version,
	}

	if err := appData.Merge(fields); err != nil {
		return fmt.Errorf("cannot write schema version: %w", err)
	}

	return nil
}

// validate проверяет, что версии миграций идут подряд с base+1.
func validate(base int, list []Migration) error {
	for i, migration := range list {
		if migration.Version != base+i+1 {
			return fmt.Errorf("migration #%d has version %d, expected %d", i, migration.Version, base+i+1)
		}

		if migration.Up == nil {
			return fmt.Errorf("migration %d has no Up function", migration.Version)
		}
	}

	return nil
}

// latestVersion возвращает версию последней миграции списка, который начинается с версии base+1.
func latestVersion(base int, list []Migration) int {
	return base + len(list)
}
