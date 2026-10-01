package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := WriteFileAtomic(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(path, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "second" {
		t.Errorf("content = %q, want second", data)
	}

	// Права существующего файла сохраняются.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
}
