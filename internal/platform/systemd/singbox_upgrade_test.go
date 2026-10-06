package systemd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/updater"
)

// fakeSingBoxBinary создает скрипт, который печатает вывод sing-box version с версией value.
func fakeSingBoxBinary(t *testing.T, value string) string {
	t.Helper()

	// Скрипт вместо бинарника запускается только в unix (платформа systemd — Linux).
	if runtime.GOOS == "windows" {
		t.Skip("shell script as sing-box binary requires unix")
	}

	path := filepath.Join(t.TempDir(), "sing-box")
	script := "#!/bin/sh\necho 'sing-box version " + value + "'\necho\necho 'Environment: go1.26.8 linux/amd64'\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	return path
}

// TestPrepareSingBox проверяет, что обновляется только sing-box-lx старше нужной версии.
func TestPrepareSingBox(t *testing.T) {
	cases := map[string]bool{
		"1.14.1-lx.8":         true,
		"1.14.2-lx.11-mac.1":  false,
		"1.15.0-lx.1":         false,
		"1.14.2":              false,
		"1.14.2-lx.11-mac.1x": false,
	}

	for current, upgrade := range cases {
		target := &Target{
			opts: TargetOptions{
				Unit:          "sing-box-configurer",
				Binary:        "/usr/local/bin/sing-box-configurer",
				BackupPaths:   nil,
				SingBoxUnit:   defaultSingBoxUnit,
				SingBoxBinary: fakeSingBoxBinary(t, current),
			},
			log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}

		journal := &updater.Journal{
			ID:    "test",
			State: map[string]string{},
		}

		target.prepareSingBox(context.Background(), journal)

		if got := journal.State[stateSingBoxNew] != ""; got != upgrade {
			t.Errorf("sing-box %s: upgrade = %v, want %v", current, got, upgrade)
		}
	}
}

// TestExtractSingBox проверяет распаковку sing-box из архива релиза sing-box-lx.
func TestExtractSingBox(t *testing.T) {
	archive := makeArchive(t, map[string]string{
		"sing-box-1.14.2-lx.11-mac.1-linux-amd64/sing-box": "binary",
		"sing-box-1.14.2-lx.11-mac.1-linux-amd64/LICENSE":  "license",
	})

	dir := t.TempDir()

	if err := extractBinaries(archive, dir, []string{"sing-box"}); err != nil {
		t.Fatal(err)
	}

	if data, err := os.ReadFile(filepath.Join(dir, "sing-box")); err != nil || string(data) != "binary" {
		t.Errorf("sing-box = %q, %v", data, err)
	}

	if version, err := singBoxVersion(context.Background(), fakeSingBoxBinary(t, "1.14.2-lx.11-mac.1")); err != nil || version != "1.14.2-lx.11-mac.1" {
		t.Errorf("singBoxVersion = %q, %v", version, err)
	}
}
