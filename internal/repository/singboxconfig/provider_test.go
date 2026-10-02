package singboxconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBackups проверяет список резервных копий и чтение копии по имени.
func TestBackups(t *testing.T) {
	dir := t.TempDir()
	provider := NewProvider(filepath.Join(dir, "config.json"), filepath.Join(dir, "backups"))

	backups, err := provider.Backups()
	if err != nil || len(backups) != 0 {
		t.Fatalf("backups without dir = %v, %v", backups, err)
	}

	if err = provider.Write([]byte(`{"log":{"level":"warn"}}`)); err != nil {
		t.Fatal(err)
	}

	first, err := provider.Backup()
	if err != nil {
		t.Fatal(err)
	}

	// Имена копий содержат время с миллисекундами.
	time.Sleep(5 * time.Millisecond)

	if err = provider.Write([]byte(`{"log":{"level":"info"}}`)); err != nil {
		t.Fatal(err)
	}

	second, err := provider.Backup()
	if err != nil {
		t.Fatal(err)
	}

	// Чужие файлы в каталоге копий не попадают в список.
	if err = os.WriteFile(filepath.Join(dir, "backups", "app-20260101-000000.000.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	backups, err = provider.Backups()
	if err != nil {
		t.Fatal(err)
	}

	if len(backups) != 2 || backups[0].Name != filepath.Base(second) || backups[1].Name != filepath.Base(first) {
		t.Fatalf("backups = %+v", backups)
	}

	data, err := provider.ReadBackup(backups[1].Name)
	if err != nil || string(data) != `{"log":{"level":"warn"}}` {
		t.Errorf("backup content = %s, %v", data, err)
	}

	for _, name := range []string{"../config.json", "app-20260101-000000.000.json", "sing-box-20260101-000000.000.json"} {
		if _, err = provider.ReadBackup(name); !errors.Is(err, ErrBackupNotFound) {
			t.Errorf("ReadBackup(%q) = %v, want ErrBackupNotFound", name, err)
		}
	}
}
