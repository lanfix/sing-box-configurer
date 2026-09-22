package config

import (
	"encoding/json"
	"os"
)

type AppConfig struct {
	AppDataPath         string `json:"app_data_path"` // Новый путь для app.json
	ListenAddr          string `json:"listen_addr"`
	DockerControllerURL string `json:"docker_controller_url"`
	// DockerControllerAPIKey — ключ API docker-controller (X-API-Key), обязателен для обновлений.
	DockerControllerAPIKey string `json:"docker_controller_api_key"`
	SourceListsProxyUrl    string `json:"source_lists_proxy_url"`
	SingBoxConfigPath      string `json:"sing_box_config_path"`
	ClashAPIBaseURL        string `json:"clash_api_base_url"`
	ClashAPISecret         string `json:"clash_api_secret"`
}

// Read читает конфигурацию из файла path. Поля, которых нет в файле, получают значения по умолчанию.
func Read(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// json.Unmarshal перезапишет только поля, заданные в файле.
	cfg := &AppConfig{
		AppDataPath:            "app.json",
		ListenAddr:             ":8080",
		DockerControllerURL:    "http://docker-controller:8081",
		DockerControllerAPIKey: "",
		SourceListsProxyUrl:    "",
		SingBoxConfigPath:      "/etc/sing-box/config.json",
		ClashAPIBaseURL:        "http://127.0.0.1:9090",
		ClashAPISecret:         "",
	}

	if err = json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
