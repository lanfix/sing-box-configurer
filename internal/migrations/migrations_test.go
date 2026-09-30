package migrations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// testBase — версия схемы, с которой начинается тестовый список миграций.
const testBase = 6

// fakeSingBox хранит конфиг sing-box в памяти.
type fakeSingBox struct {
	config map[string]any
	writes int
}

func (f *fakeSingBox) GetActualConfigParsed() (map[string]any, error) {
	return f.config, nil
}

func (f *fakeSingBox) WriteActualConfig(config map[string]any) error {
	f.config = config
	f.writes++

	return nil
}

// newFakeSingBox создает пустой конфиг sing-box в памяти.
func newFakeSingBox() *fakeSingBox {
	return &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}
}

// newAppData создает app.json с содержимым content (пустая строка — файла нет).
func newAppData(t *testing.T, content string) (*appdata.File, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.json")

	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return appdata.NewFile(path), path
}

// readFields читает поля app.json.
func readFields(t *testing.T, path string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]any

	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}

	return fields
}

// testList — миграции для тестов: переименовывают поле и добавляют outbound в конфиг sing-box.
var testList = []Migration{
	{
		Version: testBase + 1,
		Name:    "rename old_field",
		Up: func(state *State) error {
			if raw, ok := state.AppData["old_field"]; ok {
				state.AppData["new_field"] = raw
				delete(state.AppData, "old_field")
			}

			return nil
		},
	},
	{
		Version: testBase + 2,
		Name:    "add outbound",
		Up: func(state *State) error {
			config, err := state.SingBoxConfig()
			if err != nil {
				return err
			}

			config["outbounds"] = []any{
				map[string]any{
					"type": "direct",
					"tag":  "direct",
				},
			}
			state.MarkSingBoxConfigChanged()

			return nil
		},
	},
}

// TestRunFreshInstall проверяет, что новая инсталляция сразу получает последнюю версию схемы.
func TestRunFreshInstall(t *testing.T) {
	appData, path := newAppData(t, "")
	singBox := newFakeSingBox()

	result, err := run(appData, singBox, testBase, testList)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Applied) != 0 || singBox.writes != 0 {
		t.Errorf("fresh install must not run migrations: %+v, writes=%d", result, singBox.writes)
	}

	if readFields(t, path)["schema_version"] != float64(testBase+2) {
		t.Errorf("schema_version must be %d", testBase+2)
	}
}

// TestRunFreshInstallRegistry проверяет, что новая инсталляция получает версию схемы реестра.
func TestRunFreshInstallRegistry(t *testing.T) {
	appData, path := newAppData(t, "")

	if _, err := Run(appData, newFakeSingBox()); err != nil {
		t.Fatal(err)
	}

	if readFields(t, path)["schema_version"] != float64(LatestVersion()) {
		t.Errorf("schema_version must be %d", LatestVersion())
	}
}

// TestRunPendingMigrations проверяет применение недостающих миграций к данным базовой версии.
func TestRunPendingMigrations(t *testing.T) {
	appData, path := newAppData(t, `{"schema_version": 6, "rules": [], "old_field": "value"}`)
	singBox := newFakeSingBox()

	result, err := run(appData, singBox, testBase, testList)
	if err != nil {
		t.Fatal(err)
	}

	if result.FromVersion != testBase || result.ToVersion != testBase+2 || len(result.Applied) != 2 {
		t.Errorf("unexpected result: %+v", result)
	}

	fields := readFields(t, path)

	if _, ok := fields["old_field"]; ok {
		t.Error("old_field must be removed")
	}

	if fields["new_field"] != "value" || fields["schema_version"] != float64(testBase+2) {
		t.Errorf("unexpected fields: %v", fields)
	}

	if singBox.writes != 1 || singBox.config["outbounds"] == nil {
		t.Errorf("sing-box config must be written once: writes=%d", singBox.writes)
	}

	// Повторный запуск ничего не делает.
	result, err = run(appData, singBox, testBase, testList)
	if err != nil || len(result.Applied) != 0 || singBox.writes != 1 {
		t.Errorf("second run must be no-op: %+v, %v, writes=%d", result, err, singBox.writes)
	}
}

// TestRunNewerSchema проверяет отказ работать с данными более новой версии.
func TestRunNewerSchema(t *testing.T) {
	appData, _ := newAppData(t, `{"schema_version": 9}`)

	if _, err := run(appData, newFakeSingBox(), testBase, testList); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("expected ErrNewerSchema, got %v", err)
	}
}

// TestRunOldSchema проверяет отказ работать с данными старше базовой версии.
func TestRunOldSchema(t *testing.T) {
	for _, content := range []string{`{"rules": []}`, `{"schema_version": 5}`} {
		appData, _ := newAppData(t, content)

		if _, err := run(appData, newFakeSingBox(), testBase, testList); !errors.Is(err, ErrOldSchema) {
			t.Errorf("%s: expected ErrOldSchema, got %v", content, err)
		}
	}
}

// TestRunFailureKeepsFiles проверяет, что при ошибке миграции файлы не меняются.
func TestRunFailureKeepsFiles(t *testing.T) {
	original := `{"schema_version": 6, "old_field": "value"}`
	appData, path := newAppData(t, original)
	singBox := newFakeSingBox()

	failing := append([]Migration{}, testList...)
	failing = append(failing, Migration{
		Version: testBase + 3,
		Name:    "broken",
		Up: func(_ *State) error {
			return errors.New("boom")
		},
	})

	if _, err := run(appData, singBox, testBase, failing); err == nil {
		t.Fatal("expected error")
	}

	raw, _ := os.ReadFile(path)

	if string(raw) != original || singBox.writes != 0 {
		t.Errorf("files must stay untouched: app.json=%s writes=%d", raw, singBox.writes)
	}
}

// TestValidate проверяет контроль нумерации миграций.
func TestValidate(t *testing.T) {
	if err := validate(baseVersion, registry); err != nil {
		t.Fatalf("registry is invalid: %v", err)
	}

	broken := []Migration{
		{
			Version: testBase + 2,
			Name:    "gap",
			Up: func(_ *State) error {
				return nil
			},
		},
	}

	if err := validate(testBase, broken); err == nil {
		t.Error("expected error for version gap")
	}
}
