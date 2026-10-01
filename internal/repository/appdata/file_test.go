package appdata

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMergeKeepsForeignFields проверяет, что Merge обновляет только свои поля и сохраняет чужие.
func TestMergeKeepsForeignFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")

	if err := os.WriteFile(path, []byte(`{"rules": [{"id": "1"}], "groups": [{"name": "default"}]}`), 0644); err != nil {
		t.Fatal(err)
	}

	file := NewFile(path)

	happ := struct {
		Happ map[string]string `json:"happ"`
	}{
		Happ: map[string]string{
			"installation_id": "ID",
		},
	}

	if err := file.Merge(happ); err != nil {
		t.Fatal(err)
	}

	rules := struct {
		Rules []map[string]string `json:"rules"`
	}{
		Rules: []map[string]string{},
	}

	if err := file.Merge(rules); err != nil {
		t.Fatal(err)
	}

	var result struct {
		Rules  []map[string]string `json:"rules"`
		Groups []map[string]string `json:"groups"`
		Happ   map[string]string   `json:"happ"`
	}

	if err := file.Read(&result); err != nil {
		t.Fatal(err)
	}

	if len(result.Rules) != 0 {
		t.Errorf("rules = %v, want empty", result.Rules)
	}

	if len(result.Groups) != 1 || result.Groups[0]["name"] != "default" {
		t.Errorf("groups = %v, want untouched", result.Groups)
	}

	if result.Happ["installation_id"] != "ID" {
		t.Errorf("happ = %v, want installation_id", result.Happ)
	}
}

// TestReadNotExist проверяет, что отсутствующий и пустой файл возвращают ErrNotExist.
func TestReadNotExist(t *testing.T) {
	dir := t.TempDir()

	var v map[string]any

	if err := NewFile(filepath.Join(dir, "missing.json")).Read(&v); err != ErrNotExist {
		t.Errorf("missing file: err = %v, want ErrNotExist", err)
	}

	empty := filepath.Join(dir, "empty.json")

	if err := os.WriteFile(empty, []byte("  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewFile(empty).Read(&v); err != ErrNotExist {
		t.Errorf("empty file: err = %v, want ErrNotExist", err)
	}
}

// TestWriteRestrictsPermissions проверяет, что app.json со старыми правами 0644 после записи доступен только владельцу.
func TestWriteRestrictsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions are not supported")
	}

	path := filepath.Join(t.TempDir(), "app.json")

	if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewFile(path).Merge(map[string]string{"key": "value"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
}
