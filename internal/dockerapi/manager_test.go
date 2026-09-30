package dockerapi

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
)

// TestCleanInheritedConfig проверяет, что значения старого образа не переносятся в новый контейнер.
func TestCleanInheritedConfig(t *testing.T) {
	old := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID: "abcdef1234567890",
		},
		Config: &container.Config{
			Hostname:   "abcdef123456",
			Image:      "lanfix/app:v1",
			Env:        []string{"PATH=/usr/bin", "CUSTOM=1"},
			Cmd:        []string{"/app/app"},
			WorkingDir: "/app",
			Labels: map[string]string{
				"org.opencontainers.image.version": "v1",
				"com.docker.compose.service":       "app",
				"com.docker.compose.image":         "sha256:old",
				"app":                              "custom",
			},
		},
	}

	oldImage := image.InspectResponse{
		Config: &dockerspec.DockerOCIImageConfig{},
	}
	oldImage.Config.Env = []string{"PATH=/usr/bin"}
	oldImage.Config.Cmd = []string{"/app/app"}
	oldImage.Config.WorkingDir = "/app"
	oldImage.Config.Labels = map[string]string{
		"org.opencontainers.image.version": "v1",
	}

	config := CleanInheritedConfig(old, oldImage)

	if len(config.Env) != 1 || config.Env[0] != "CUSTOM=1" {
		t.Errorf("env = %v, want only CUSTOM=1", config.Env)
	}

	if config.Cmd != nil || config.WorkingDir != "" || config.Hostname != "" {
		t.Errorf("cmd/workdir/hostname must be reset: %+v", config)
	}

	if _, ok := config.Labels["org.opencontainers.image.version"]; ok {
		t.Error("image version label must not be inherited")
	}

	if _, ok := config.Labels["com.docker.compose.image"]; ok {
		t.Error("compose image label must be removed")
	}

	if config.Labels["com.docker.compose.service"] != "app" || config.Labels["app"] != "custom" {
		t.Errorf("container labels must be kept: %v", config.Labels)
	}

	if old.Config.Labels["com.docker.compose.image"] == "" || len(old.Config.Env) != 2 {
		t.Error("original config must not be modified")
	}
}

// TestCleanInheritedConfigCustomCmd проверяет, что явно заданная команда сохраняется.
func TestCleanInheritedConfigCustomCmd(t *testing.T) {
	old := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID: "abcdef1234567890",
		},
		Config: &container.Config{
			Hostname: "custom-host",
			Cmd:      []string{"-D", "/var/lib/sing-box", "run"},
		},
	}

	oldImage := image.InspectResponse{
		Config: &dockerspec.DockerOCIImageConfig{},
	}
	oldImage.Config.Cmd = []string{"run"}

	config := CleanInheritedConfig(old, oldImage)

	if len(config.Cmd) != 3 || config.Hostname != "custom-host" {
		t.Errorf("custom cmd and hostname must be kept: %+v", config)
	}
}

// TestEndpointsFrom проверяет перенос сетей без alias с ID старого контейнера.
func TestEndpointsFrom(t *testing.T) {
	old := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID: "abcdef1234567890",
		},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"deploy_default": {
					Aliases:   []string{"sing-box-configurer", "abcdef123456"},
					IPAddress: "172.18.0.3",
				},
			},
		},
	}

	endpoints := EndpointsFrom(old).EndpointsConfig

	settings, ok := endpoints["deploy_default"]
	if !ok {
		t.Fatal("network must be kept")
	}

	if len(settings.Aliases) != 1 || settings.Aliases[0] != "sing-box-configurer" {
		t.Errorf("aliases = %v", settings.Aliases)
	}

	if settings.IPAddress != "" {
		t.Error("runtime IP must not be copied")
	}
}

// TestValidateRollbackName проверяет, что при откате можно трогать только rollback-копии контейнера.
func TestValidateRollbackName(t *testing.T) {
	valid := map[string]string{
		"sing-box-configurer": "sing-box-configurer-rollback-20260922-150405",
		"sing-box":            "sing-box-rollback-abc_1",
	}

	for name, rollback := range valid {
		if err := ValidateRollbackName(name, rollback); err != nil {
			t.Errorf("%s/%s must be valid: %v", name, rollback, err)
		}
	}

	invalid := map[string]string{
		"sing-box-configurer": "sing-box",
		"sing-box":            "sing-box-configurer-rollback-1",
		"sing-box-lx":         "sing-box-lx-rollback-",
		"app":                 "app-rollback-1/../x",
	}

	for name, rollback := range invalid {
		if err := ValidateRollbackName(name, rollback); err == nil {
			t.Errorf("%s/%s must be invalid", name, rollback)
		}
	}
}
