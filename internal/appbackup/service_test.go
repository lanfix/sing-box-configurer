package appbackup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/migrations"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// fakeSingBox отдает миграциям пустой конфиг sing-box.
type fakeSingBox struct{}

func (fakeSingBox) GetActualConfigParsed() (map[string]any, error) {
	return map[string]any{}, nil
}

func (fakeSingBox) WriteActualConfig(map[string]any) error {
	return errors.New("import must not write sing-box config")
}

// currentData — данные текущей установки.
const currentData = `{
  "schema_version": %d,
  "auth": {"enabled": true, "username": "admin", "password_hash": "current-hash", "session_key": "current-key"},
  "settings": {"log_level": "warn", "security": {"check_host": true, "allowed_hosts": ["panel.current"]}},
  "groups": [{"name": "default"}]
}`

// importData — данные из файла импорта.
const importData = `{
  "schema_version": %d,
  "auth": {"enabled": true, "username": "other", "password_hash": "file-hash", "session_key": "file-key"},
  "settings": {"log_level": "debug", "security": {"check_host": true, "allowed_hosts": ["panel.file"]}},
  "groups": [{"name": "default"}, {"name": "video"}],
  "rules": [{"id": "1", "type": "domain", "value": "example.com", "group": "video"}]
}`

// newTestService создает сервис с текущими данными в app.json и счетчиком проверок.
func newTestService(t *testing.T, validate Validator) (*Service, *appdata.File, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")

	if err := os.WriteFile(path, fmt.Appendf(nil, currentData, migrations.LatestVersion()), 0600); err != nil {
		t.Fatal(err)
	}

	appData := appdata.NewFile(path)

	if validate == nil {
		validate = func(string) error {
			return nil
		}
	}

	return NewService(appData, fakeSingBox{}, filepath.Join(dir, "backups"), validate), appData, dir
}

// readField разбирает поле key app.json.
func readField(t *testing.T, appData *appdata.File, key string, v any) {
	t.Helper()

	fields, err := appData.ReadRaw()
	if err != nil {
		t.Fatal(err)
	}

	if err = json.Unmarshal(fields[key], v); err != nil {
		t.Fatalf("%s: %v", key, err)
	}
}

// TestImportKeepsAccess проверяет, что по умолчанию вход в панель и защита адреса остаются текущими.
func TestImportKeepsAccess(t *testing.T) {
	service, appData, dir := newTestService(t, nil)

	result, err := service.Import(fmt.Appendf(nil, importData, migrations.LatestVersion()), ImportOptions{
		ReplaceAccess: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	var auth struct {
		Username string `json:"username"`
	}

	readField(t, appData, "auth", &auth)

	var settings struct {
		LogLevel string `json:"log_level"`
		Security struct {
			AllowedHosts []string `json:"allowed_hosts"`
		} `json:"security"`
	}

	readField(t, appData, "settings", &settings)

	if auth.Username != "admin" || settings.Security.AllowedHosts[0] != "panel.current" {
		t.Errorf("access must be kept: auth=%+v settings=%+v", auth, settings)
	}

	if settings.LogLevel != "debug" {
		t.Errorf("settings must be imported: %+v", settings)
	}

	var groups []map[string]any

	readField(t, appData, "groups", &groups)

	if len(groups) != 2 {
		t.Errorf("groups = %v", groups)
	}

	if result.Backup == "" || !strings.HasPrefix(result.Backup, filepath.Join(dir, "backups")) {
		t.Errorf("backup = %q", result.Backup)
	}

	backup, err := os.ReadFile(result.Backup)
	if err != nil || !strings.Contains(string(backup), "current-hash") {
		t.Errorf("backup must contain previous data: %v", err)
	}

	// После импорта запись в app.json запрещена до перезапуска, повторный импорт — тоже.
	if err = appData.Merge(map[string]any{"groups": []any{}}); !errors.Is(err, appdata.ErrFrozen) {
		t.Errorf("merge after import = %v, want ErrFrozen", err)
	}

	if _, err = service.Import(fmt.Appendf(nil, importData, migrations.LatestVersion()), ImportOptions{}); !errors.Is(err, ErrImported) {
		t.Errorf("second import = %v, want ErrImported", err)
	}
}

// TestImportReplacesAccess проверяет перенос входа в панель и защиты адреса из файла.
func TestImportReplacesAccess(t *testing.T) {
	service, appData, _ := newTestService(t, nil)

	if _, err := service.Import(fmt.Appendf(nil, importData, migrations.LatestVersion()), ImportOptions{
		ReplaceAccess: true,
	}); err != nil {
		t.Fatal(err)
	}

	var auth struct {
		Username     string `json:"username"`
		PasswordHash string `json:"password_hash"`
	}

	readField(t, appData, "auth", &auth)

	if auth.Username != "other" || auth.PasswordHash != "file-hash" {
		t.Errorf("auth must be replaced: %+v", auth)
	}
}

// TestImportRejects проверяет, что неподходящие файлы не меняют данные.
func TestImportRejects(t *testing.T) {
	latest := migrations.LatestVersion()

	cases := map[string]struct {
		data string
		want error
	}{
		"not json":     {data: "not json", want: ErrInvalidData},
		"array":        {data: "[1, 2]", want: ErrInvalidData},
		"foreign json": {data: `{"name": "package", "version": "1.0.0"}`, want: ErrInvalidData},
		"newer schema": {data: fmt.Sprintf(importData, latest+1), want: ErrUnsupportedSchema},
		"older schema": {data: fmt.Sprintf(importData, migrations.BaseVersion()-1), want: ErrUnsupportedSchema},
	}

	for name, tc := range cases {
		service, appData, _ := newTestService(t, nil)

		before, _ := os.ReadFile(appData.Path())

		if _, err := service.Import([]byte(tc.data), ImportOptions{}); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}

		after, _ := os.ReadFile(appData.Path())

		if string(before) != string(after) {
			t.Errorf("%s: app data must not change", name)
		}
	}

	// Данные, которые не загружаются менеджерами, отклоняются.
	service, _, _ := newTestService(t, func(string) error {
		return errors.New("broken section")
	})

	if _, err := service.Import(fmt.Appendf(nil, importData, latest), ImportOptions{}); !errors.Is(err, ErrInvalidData) || !strings.Contains(err.Error(), "broken section") {
		t.Errorf("validation error = %v", err)
	}
}

// TestInspect проверяет сводку файла импорта.
func TestInspect(t *testing.T) {
	service, _, _ := newTestService(t, nil)

	summary, err := service.Inspect(fmt.Appendf(nil, importData, migrations.LatestVersion()))
	if err != nil {
		t.Fatal(err)
	}

	if summary.Counts["groups"] != 2 || summary.Counts["rules"] != 1 || !summary.HasAuth || summary.Username != "other" {
		t.Errorf("summary = %+v", summary)
	}

	summary, err = service.Inspect(fmt.Appendf(nil, importData, migrations.LatestVersion()+3))
	if !errors.Is(err, ErrUnsupportedSchema) || summary == nil || summary.SchemaVersion != migrations.LatestVersion()+3 {
		t.Errorf("newer schema: summary = %+v, err = %v", summary, err)
	}
}

// TestExport проверяет, что выгрузка содержит все поля app.json.
func TestExport(t *testing.T) {
	service, _, _ := newTestService(t, nil)

	data, err := service.Export()
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage

	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"schema_version", "auth", "settings", "groups"} {
		if fields[key] == nil {
			t.Errorf("export must contain %s", key)
		}
	}
}
