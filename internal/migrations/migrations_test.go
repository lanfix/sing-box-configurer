package migrations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

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
		Version: 1,
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
		Version: 2,
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
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	result, err := run(appData, singBox, testList)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Applied) != 0 || singBox.writes != 0 {
		t.Errorf("fresh install must not run migrations: %+v, writes=%d", result, singBox.writes)
	}

	if readFields(t, path)["schema_version"] != float64(2) {
		t.Errorf("schema_version must be 2")
	}
}

// TestRunLegacyData проверяет применение всех миграций к данным без schema_version.
func TestRunLegacyData(t *testing.T) {
	appData, path := newAppData(t, `{"rules": [], "old_field": "value"}`)
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	result, err := run(appData, singBox, testList)
	if err != nil {
		t.Fatal(err)
	}

	if result.FromVersion != 0 || result.ToVersion != 2 || len(result.Applied) != 2 {
		t.Errorf("unexpected result: %+v", result)
	}

	fields := readFields(t, path)

	if _, ok := fields["old_field"]; ok {
		t.Error("old_field must be removed")
	}

	if fields["new_field"] != "value" || fields["schema_version"] != float64(2) {
		t.Errorf("unexpected fields: %v", fields)
	}

	if singBox.writes != 1 || singBox.config["outbounds"] == nil {
		t.Errorf("sing-box config must be written once: writes=%d", singBox.writes)
	}

	// Повторный запуск ничего не делает.
	result, err = run(appData, singBox, testList)
	if err != nil || len(result.Applied) != 0 || singBox.writes != 1 {
		t.Errorf("second run must be no-op: %+v, %v, writes=%d", result, err, singBox.writes)
	}
}

// TestRunNewerSchema проверяет отказ работать с данными более новой версии.
func TestRunNewerSchema(t *testing.T) {
	appData, _ := newAppData(t, `{"schema_version": 5}`)
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	if _, err := run(appData, singBox, testList); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("expected ErrNewerSchema, got %v", err)
	}
}

// TestRunFailureKeepsFiles проверяет, что при ошибке миграции файлы не меняются.
func TestRunFailureKeepsFiles(t *testing.T) {
	original := `{"old_field": "value"}`
	appData, path := newAppData(t, original)
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	failing := append([]Migration{}, testList...)
	failing = append(failing, Migration{
		Version: 3,
		Name:    "broken",
		Up: func(_ *State) error {
			return errors.New("boom")
		},
	})

	if _, err := run(appData, singBox, failing); err == nil {
		t.Fatal("expected error")
	}

	raw, _ := os.ReadFile(path)

	if string(raw) != original || singBox.writes != 0 {
		t.Errorf("files must stay untouched: app.json=%s writes=%d", raw, singBox.writes)
	}
}

// TestValidate проверяет контроль нумерации миграций.
func TestValidate(t *testing.T) {
	if err := validate(registry); err != nil {
		t.Fatalf("registry is invalid: %v", err)
	}

	broken := []Migration{
		{
			Version: 2,
			Name:    "gap",
			Up: func(_ *State) error {
				return nil
			},
		},
	}

	if err := validate(broken); err == nil {
		t.Error("expected error for version gap")
	}
}

// TestMigrateGroupDNSServers проверяет перенос DNS-серверов групп из ручных DNS-правил.
func TestMigrateGroupDNSServers(t *testing.T) {
	appData, path := newAppData(t, `{
		"schema_version": 2,
		"groups": [
			{"name": "default", "description": "Группа по умолчанию"},
			{"name": "claude", "dns_server": "google"},
			{"name": "block"}
		]
	}`)

	singBox := &fakeSingBox{
		config: map[string]any{
			"dns": map[string]any{
				"rules": []any{
					map[string]any{
						"rule_set": "configurer-default",
						"server":   "cloudflare",
					},
					map[string]any{
						"rule_set": "configurer-claude",
						"server":   "cloudflare",
					},
				},
			},
		},
		writes: 0,
	}

	if _, err := run(appData, singBox, registry); err != nil {
		t.Fatal(err)
	}

	groups, _ := readFields(t, path)["groups"].([]any)
	servers := map[string]any{}

	for _, item := range groups {
		group := item.(map[string]any)
		servers[group["name"].(string)] = group["dns_server"]
	}

	// Уже заданный DNS-сервер не перезаписывается, у группы без правила он не появляется.
	want := map[string]any{
		"default": "cloudflare",
		"claude":  "google",
		"block":   nil,
	}

	for name, server := range want {
		if servers[name] != server {
			t.Errorf("group %s dns_server = %v, want %v", name, servers[name], server)
		}
	}

	if singBox.writes != 0 {
		t.Errorf("sing-box config must not be written, writes=%d", singBox.writes)
	}
}
