package config

import (
	"encoding/json"
	"os"
)

type AppConfig struct {
	RulesPath           string `json:"rules_path"`
	ListenAddr          string `json:"listen_addr"`
	DockerControllerURL string `json:"docker_controller_url"`
	SourceListsProxyUrl string `json:"source_lists_proxy_url"`
	SingBoxConfigPath   string `json:"sing_box_config_path"`
}

func Read[T any](path string) (*T, error) {
	data, readFileErr := os.ReadFile(path)
	if readFileErr != nil {
		return nil, readFileErr
	}

	var cfg T

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
