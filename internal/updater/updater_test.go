package updater

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/version"
)

// fakeTarget — платформа в памяти: «версия» конфигуратора — строка, данные — файл в root.
type fakeTarget struct {
	root    string
	running string
	calls   []string

	replaceErr error
}

func (f *fakeTarget) Root() string {
	return f.root
}

func (f *fakeTarget) Prepare(_ context.Context, journal *Journal) ([]string, error) {
	f.calls = append(f.calls, "prepare")
	journal.FromVersion = f.running

	return []string{"data"}, nil
}

func (f *fakeTarget) Download(_ context.Context, _ *Journal) error {
	f.calls = append(f.calls, "download")

	return nil
}

func (f *fakeTarget) Replace(_ context.Context, journal *Journal) error {
	f.calls = append(f.calls, "replace")
	journal.State["previous"] = f.running

	// Новая версия «мигрирует» данные.
	if err := os.WriteFile(filepath.Join(f.root, "data", "app.json"), []byte("migrated"), 0644); err != nil {
		return err
	}

	f.running = journal.ToVersion

	return f.replaceErr
}

func (f *fakeTarget) Alive(_ context.Context, _ *Journal) error {
	return nil
}

func (f *fakeTarget) Logs(_ context.Context, _ *Journal) string {
	return "new version logs"
}

func (f *fakeTarget) Finish(_ context.Context, _ *Journal) error {
	f.calls = append(f.calls, "finish")

	return nil
}

func (f *fakeTarget) Restore(_ context.Context, journal *Journal) error {
	f.calls = append(f.calls, "restore")
	f.running = journal.State["previous"]

	return nil
}

func (f *fakeTarget) StartPrevious(_ context.Context, _ *Journal) error {
	f.calls = append(f.calls, "start-previous")

	return nil
}

func (f *fakeTarget) Commit(_ context.Context, _ *Journal) error {
	f.calls = append(f.calls, "commit")

	return nil
}

// runUpdate выполняет обновление с фейковой платформой и возвращает итоговую фазу из журнала.
func runUpdate(t *testing.T, target *fakeTarget, healthVersion func() string) *Journal {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": healthVersion(),
		})
	}))
	defer server.Close()

	updatesDir := filepath.Join(target.root, UpdatesDirName)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	u, err := New(Options{
		UpdateID:   "20260930-120000",
		UpdatesDir: updatesDir,
		HealthURL:  server.URL,
	}, target, logger)
	if err != nil {
		t.Fatal(err)
	}

	u.Run(context.Background())

	journal, err := loadJournal(updatesDir, "20260930-120000")
	if err != nil || journal == nil {
		t.Fatalf("journal must be saved: %v", err)
	}

	return journal
}

// newFakeTarget создает платформу с данными в data/app.json.
func newFakeTarget(t *testing.T) *fakeTarget {
	t.Helper()

	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "data", "app.json"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	return &fakeTarget{
		root:       root,
		running:    "v0.1.0",
		calls:      nil,
		replaceErr: nil,
	}
}

// setVersion подменяет версию updater на время теста.
func setVersion(t *testing.T, value string) {
	t.Helper()

	previous := version.Version
	version.Version = value

	t.Cleanup(func() {
		version.Version = previous
	})
}

// TestRunSucceeded проверяет полный проход обновления.
func TestRunSucceeded(t *testing.T) {
	setVersion(t, "v0.2.0")

	target := newFakeTarget(t)
	journal := runUpdate(t, target, func() string {
		return target.running
	})

	if journal.Phase != PhaseSucceeded || journal.FromVersion != "v0.1.0" || journal.ToVersion != "v0.2.0" {
		t.Fatalf("unexpected journal: %+v", journal)
	}

	want := []string{"prepare", "download", "replace", "finish", "commit"}

	if !slices.Equal(target.calls, want) {
		t.Errorf("calls = %v, want %v", target.calls, want)
	}

	if len(journal.Backups) != 1 || journal.Backups[0].Path != "data/app.json" {
		t.Errorf("backups = %+v", journal.Backups)
	}
}

// TestRunRollback проверяет откат, если новая версия не отвечает на health-check.
func TestRunRollback(t *testing.T) {
	setVersion(t, "v0.2.0")

	target := newFakeTarget(t)
	target.replaceErr = errors.New("container exited")

	journal := runUpdate(t, target, func() string {
		return "v0.1.0"
	})

	if journal.Phase != PhaseRolledBack || journal.Error == "" {
		t.Fatalf("unexpected journal: %+v", journal)
	}

	want := []string{"prepare", "download", "replace", "restore", "start-previous"}

	if !slices.Equal(target.calls, want) {
		t.Errorf("calls = %v, want %v", target.calls, want)
	}

	data, _ := os.ReadFile(filepath.Join(target.root, "data", "app.json"))

	if string(data) != "original" || target.running != "v0.1.0" {
		t.Errorf("data = %q, running = %s", data, target.running)
	}
}

// TestRunNotNewer проверяет, что обновление на ту же версию ничего не меняет.
func TestRunNotNewer(t *testing.T) {
	setVersion(t, "v0.1.0")

	target := newFakeTarget(t)
	journal := runUpdate(t, target, func() string {
		return target.running
	})

	if journal.Phase != PhaseRolledBack || !slices.Equal(target.calls, []string{"prepare"}) {
		t.Errorf("unexpected journal %+v, calls %v", journal, target.calls)
	}
}

// TestPruneUpdates проверяет, что остаются последние папки и текущая.
func TestPruneUpdates(t *testing.T) {
	dir := t.TempDir()
	names := []string{"u1", "u2", "u3", "u4"}

	for i, name := range names {
		path := filepath.Join(dir, name)

		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}

		modTime := time.Now().Add(time.Duration(i) * time.Minute)

		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}

	// Текущее обновление u1 — самое старое по времени, но должно остаться.
	if err := pruneUpdates(dir, "u1", 2); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]bool{"u1": true, "u2": false, "u3": false, "u4": true} {
		_, err := os.Stat(filepath.Join(dir, name))

		if (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", name, err == nil, want)
		}
	}
}

// TestBackupRestore проверяет, что восстановление пишет в тот же файл (inode не меняется).
func TestBackupRestore(t *testing.T) {
	root := t.TempDir()
	updateDir := t.TempDir()
	file := filepath.Join(root, "app.json")

	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	backups, err := backupFiles(root, updateDir, []string{"app.json"})
	if err != nil {
		t.Fatal(err)
	}

	before, _ := os.Stat(file)

	if err = os.WriteFile(file, []byte("migrated"), 0644); err != nil {
		t.Fatal(err)
	}

	if err = restoreFiles(root, updateDir, backups); err != nil {
		t.Fatal(err)
	}

	after, _ := os.Stat(file)
	data, _ := os.ReadFile(file)

	if string(data) != "original" {
		t.Errorf("content = %q", data)
	}

	if !os.SameFile(before, after) {
		t.Error("restore must write the same file in place")
	}
}

// TestExpandFiles проверяет, что каталог данных бэкапится файлами без служебных подкаталогов.
func TestExpandFiles(t *testing.T) {
	root := t.TempDir()

	files := map[string]string{
		"data/app.json":                    "{}",
		"data/backups/sing-box-1.json":     "{}",
		"data/.updates/x/journal.json":     "{}",
		"sing-box/config.json":             "{}",
		"sing-box/.updates/x/journal.json": "{}",
	}

	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	got := expandFiles(root, []string{"data", "sing-box", "data/app.json", "missing.json"})
	want := []string{"data/app.json", "sing-box/config.json"}

	if !slices.Equal(got, want) {
		t.Errorf("expandFiles = %v, want %v", got, want)
	}
}
