package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSplitImage проверяет разделение образа на репозиторий и тег.
func TestSplitImage(t *testing.T) {
	cases := map[string][2]string{
		"docker.io/lanfix/sing-box-configurer:v0.0.19": {"docker.io/lanfix/sing-box-configurer", "v0.0.19"},
		"lanfix/docker-controller:v0.0.1":              {"lanfix/docker-controller", "v0.0.1"},
		"registry:5000/app":                            {"registry:5000/app", "latest"},
		"registry:5000/app:v1.0.0":                     {"registry:5000/app", "v1.0.0"},
	}

	for ref, want := range cases {
		repository, tag := splitImage(ref)

		if repository != want[0] || tag != want[1] {
			t.Errorf("splitImage(%q) = %q, %q; want %q, %q", ref, repository, tag, want[0], want[1])
		}
	}
}

// TestDeployPathsRelative проверяет сопоставление путей хоста и папки деплоя.
func TestDeployPathsRelative(t *testing.T) {
	paths := deployPaths{
		hostDir:  "/opt/vpn/",
		localDir: "/deploy",
	}

	cases := map[string]string{
		"/opt/vpn/app.json":           "app.json",
		"/opt/vpn/conf/sing-box.json": "conf/sing-box.json",
		"/opt/vpn":                    "",
		"/opt/vpn2/app.json":          "",
		"/var/run/docker.sock":        "",
	}

	for hostPath, want := range cases {
		rel, ok := paths.relative(hostPath)

		if rel != want || ok != (want != "") {
			t.Errorf("relative(%q) = %q, %v; want %q", hostPath, rel, ok, want)
		}
	}
}

// TestSetComposeImageTag проверяет замену тега только у нужного образа.
func TestSetComposeImageTag(t *testing.T) {
	file := filepath.Join(t.TempDir(), "docker-compose.yaml")
	original := `services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:v0.0.10
  docker-controller:
    image: "lanfix/docker-controller:v0.0.1"
  sing-box:
    image: ghcr.io/sagernet/sing-box:v1.14.0
`

	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := setComposeImageTag(file, "docker.io/lanfix/sing-box-configurer", "v0.1.0")
	if err != nil || !changed {
		t.Fatalf("configurer: changed=%v err=%v", changed, err)
	}

	changed, err = setComposeImageTag(file, "docker.io/lanfix/docker-controller", "v0.1.0")
	if err != nil || !changed {
		t.Fatalf("controller: changed=%v err=%v", changed, err)
	}

	changed, err = setComposeImageTag(file, "docker.io/lanfix/docker-controller", "v0.1.0")
	if err != nil || changed {
		t.Fatalf("repeated update must be no-op: changed=%v err=%v", changed, err)
	}

	want := `services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:v0.1.0
  docker-controller:
    image: "lanfix/docker-controller:v0.1.0"
  sing-box:
    image: ghcr.io/sagernet/sing-box:v1.14.0
`

	got, _ := os.ReadFile(file)

	if string(got) != want {
		t.Errorf("unexpected compose file:\n%s", got)
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
	deployDir := t.TempDir()
	updateDir := t.TempDir()
	paths := deployPaths{
		hostDir:  "/host",
		localDir: deployDir,
	}

	file := filepath.Join(deployDir, "app.json")

	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	backups, err := backupFiles(paths, updateDir, []string{"app.json"})
	if err != nil {
		t.Fatal(err)
	}

	before, _ := os.Stat(file)

	if err = os.WriteFile(file, []byte("migrated"), 0644); err != nil {
		t.Fatal(err)
	}

	if err = restoreFiles(paths, updateDir, backups); err != nil {
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

// TestParseResult проверяет разбор итоговой строки job.
func TestParseResult(t *testing.T) {
	logs := `{"level":"INFO","msg":"self-update started"}
not a json line
{"level":"INFO","msg":"self-update finished","result":"rolled_back","error":"health check timeout"}
`

	result, message := parseResult(logs)

	if result != "rolled_back" || message != "health check timeout" {
		t.Errorf("parseResult = %q, %q", result, message)
	}

	if result, _ = parseResult("{}\n"); result != "" {
		t.Errorf("empty logs must have no result, got %q", result)
	}
}

// TestComposeFiles проверяет разбор лейбла со списком compose-файлов.
func TestComposeFiles(t *testing.T) {
	files := composeFiles(map[string]string{
		composeConfigFilesLabel: "/opt/vpn/docker-compose.yaml, /opt/vpn/override.yaml",
	})

	if len(files) != 2 || files[0] != "/opt/vpn/docker-compose.yaml" || files[1] != "/opt/vpn/override.yaml" {
		t.Errorf("composeFiles = %v", files)
	}

	if len(composeFiles(map[string]string{})) != 0 {
		t.Error("no label must give no files")
	}
}
