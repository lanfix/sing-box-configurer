package config

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type AppConfig struct {
	AppDataPath         string `json:"app_data_path"`
	ListenAddr          string `json:"listen_addr"`
	DockerControllerURL string `json:"docker_controller_url"`
	// DockerControllerAPIKey — ключ API docker-controller (X-API-Key), обязателен для обновлений и применения конфига.
	DockerControllerAPIKey string `json:"docker_controller_api_key"`
	SourceListsProxyUrl    string `json:"source_lists_proxy_url"`
	SingBoxConfigPath      string `json:"sing_box_config_path"`
	ClashAPIBaseURL        string `json:"clash_api_base_url"`

	// ClashAPISecret — секрет Clash API на случай, если его нет в рабочем конфиге sing-box.
	// Обычно секрет берется из рабочего конфига, куда его записывает конфигуратор.
	ClashAPISecret string `json:"clash_api_secret"`

	// RuleSetBaseURL — адрес конфигуратора, по которому sing-box забирает rule-set-ы.
	// По умолчанию http://127.0.0.1:<порт listen_addr>: sing-box работает в сети хоста.
	RuleSetBaseURL string `json:"rule_set_base_url"`

	// BackupDir — каталог резервных копий конфига sing-box. По умолчанию backups рядом с app_data_path.
	BackupDir string `json:"backup_dir"`
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
		RuleSetBaseURL:         "",
		BackupDir:              "",
	}

	if err = json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.RuleSetBaseURL == "" {
		cfg.RuleSetBaseURL = "http://127.0.0.1:" + listenPort(cfg.ListenAddr)
	}

	cfg.RuleSetBaseURL = strings.TrimSuffix(cfg.RuleSetBaseURL, "/")

	if cfg.BackupDir == "" {
		cfg.BackupDir = filepath.Join(filepath.Dir(cfg.AppDataPath), "backups")
	}

	return cfg, nil
}

// listenPort возвращает порт из адреса listen_addr (8080, если его не удалось разобрать).
func listenPort(listenAddr string) string {
	if _, port, err := net.SplitHostPort(listenAddr); err == nil && port != "" {
		return port
	}

	return "8080"
}
