package systemd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// TestParseState проверяет разбор вывода systemctl show.
func TestParseState(t *testing.T) {
	cases := []struct {
		output  string
		running bool
		failed  bool
		code    int
	}{
		{"ActiveState=active\nSubState=running\nExecMainStatus=0\n", true, false, 0},
		{"ActiveState=activating\nSubState=auto-restart\nExecMainStatus=1\n", false, true, 1},
		{"ActiveState=failed\nSubState=failed\nExecMainStatus=203\n", false, true, 203},
		{"ActiveState=inactive\nSubState=dead\nExecMainStatus=0\n", false, false, 0},
	}

	for _, c := range cases {
		state := parseState(c.output)

		if state.Running != c.running || state.Failed != c.failed || state.ExitCode != c.code {
			t.Errorf("parseState(%q) = %+v", c.output, state)
		}
	}
}

// TestFindChecksum проверяет поиск SHA-256 архива в выводе sha256sum.
func TestFindChecksum(t *testing.T) {
	checksums := []byte("aaa  sing-box-configurer_v1.0.0_linux_amd64.tar.gz\nBBB *sing-box-configurer_v1.0.0_linux_arm64.tar.gz\n")

	if sum, err := findChecksum(checksums, "sing-box-configurer_v1.0.0_linux_arm64.tar.gz"); err != nil || sum != "bbb" {
		t.Errorf("findChecksum = %q, %v", sum, err)
	}

	if _, err := findChecksum(checksums, "sing-box-configurer_v1.0.0_linux_armv7.tar.gz"); err == nil {
		t.Error("missing checksum must be an error")
	}
}

// TestExtractBinaries проверяет распаковку бинарников из архива релиза.
func TestExtractBinaries(t *testing.T) {
	archive := makeArchive(t, map[string]string{
		"sing-box-configurer_v1.0.0_linux_amd64/sing-box-configurer": "configurer",
		"sing-box-configurer_v1.0.0_linux_amd64/updater":             "updater",
		"sing-box-configurer_v1.0.0_linux_amd64/README.md":           "readme",
	})

	dir := t.TempDir()

	if err := extractBinaries(archive, dir); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{"sing-box-configurer": "configurer", "updater": "updater"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, %v", name, data, err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Error("extra files must not be extracted")
	}

	incomplete := makeArchive(t, map[string]string{
		"updater": "updater",
	})

	if err := extractBinaries(incomplete, t.TempDir()); err == nil {
		t.Error("archive without configurer binary must be rejected")
	}
}

// makeArchive создает tar.gz с файлами files.
func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer

	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)

	for name, content := range files {
		header := &tar.Header{
			Name:     name,
			Mode:     0755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}

		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}

		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}
