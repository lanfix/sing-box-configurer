package docker

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSplitImage проверяет разделение образа на репозиторий и тег.
func TestSplitImage(t *testing.T) {
	cases := map[string][2]string{
		"docker.io/lanfix/sing-box-configurer:v0.0.19": {"docker.io/lanfix/sing-box-configurer", "v0.0.19"},
		"lanfix/sing-box-lx:v1.14.1-lx.8":              {"lanfix/sing-box-lx", "v1.14.1-lx.8"},
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
		hostDir: "/opt/vpn/",
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
  other:
    image: "lanfix/sing-box-configurer-extra:v0.0.1"
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

	changed, err = setComposeImageTag(file, "docker.io/lanfix/sing-box-configurer", "v0.1.0")
	if err != nil || changed {
		t.Fatalf("repeated update must be no-op: changed=%v err=%v", changed, err)
	}

	want := `services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:v0.1.0
  other:
    image: "lanfix/sing-box-configurer-extra:v0.0.1"
  sing-box:
    image: ghcr.io/sagernet/sing-box:v1.14.0
`

	got, _ := os.ReadFile(file)

	if string(got) != want {
		t.Errorf("unexpected compose file:\n%s", got)
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

// TestDockerHubRepository проверяет определение локально собранного образа.
func TestDockerHubRepository(t *testing.T) {
	cases := map[string]bool{
		"docker.io/lanfix/sing-box-configurer:v0.9.0": false,
		"lanfix/sing-box-configurer:v0.9.0":           false,
		"deploy-sing-box-configurer":                  true,
		"registry.local:5000/lanfix/configurer:v1":    true,
	}

	for image, wantLocal := range cases {
		if _, local := dockerHubRepository(image); local != wantLocal {
			t.Errorf("dockerHubRepository(%q) local = %v, want %v", image, local, wantLocal)
		}
	}
}

// TestSetComposeSingBoxTag проверяет замену тега образа sing-box-lx, не задевая образ конфигуратора.
func TestSetComposeSingBoxTag(t *testing.T) {
	file := filepath.Join(t.TempDir(), "docker-compose.yaml")
	original := `services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:v0.14.0
  sing-box:
    image: docker.io/lanfix/sing-box-lx:v1.14.1-lx.8
`

	if err := os.WriteFile(file, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := setComposeImageTag(file, "docker.io/lanfix/sing-box-lx", "v1.14.2-lx.11-mac.1")
	if err != nil || !changed {
		t.Fatalf("sing-box: changed=%v err=%v", changed, err)
	}

	want := `services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:v0.14.0
  sing-box:
    image: docker.io/lanfix/sing-box-lx:v1.14.2-lx.11-mac.1
`

	if got, _ := os.ReadFile(file); string(got) != want {
		t.Errorf("unexpected compose file:\n%s", got)
	}
}
