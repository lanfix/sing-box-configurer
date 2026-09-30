package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lanfix/sing-box-configurer/internal/platform"
)

// AppConfig — конфигурация сервиса. Поля, которых нет в файле, получают значения по умолчанию для платформы.
type AppConfig struct {
	// Platform — способ установки: docker или systemd. Пустое значение — определяется автоматически
	// (docker, если сервис запущен в контейнере).
	Platform string `json:"platform"`

	AppDataPath         string `json:"app_data_path"`
	ListenAddr          string `json:"listen_addr"`
	SourceListsProxyUrl string `json:"source_lists_proxy_url"`
	SingBoxConfigPath   string `json:"sing_box_config_path"`
	ClashAPIBaseURL     string `json:"clash_api_base_url"`

	// ClashAPISecret — секрет Clash API на случай, если его нет в рабочем конфиге sing-box.
	// Обычно секрет берется из рабочего конфига, куда его записывает конфигуратор.
	ClashAPISecret string `json:"clash_api_secret"`

	// RuleSetBaseURL — адрес конфигуратора, по которому sing-box забирает rule-set-ы.
	// По умолчанию http://127.0.0.1:<порт listen_addr>: sing-box работает в сети хоста.
	RuleSetBaseURL string `json:"rule_set_base_url"`

	// BackupDir — каталог резервных копий конфига sing-box. По умолчанию backups рядом с app_data_path.
	BackupDir string `json:"backup_dir"`

	// SourcesProxyPort — порт служебного inbound-а sing-box, через который загружаются URL-источники
	// с detour. Конфигуратор подключается к нему по хосту clash_api_base_url.
	SourcesProxyPort int `json:"sources_proxy_port"`

	// SourcesProxyListen — адрес, который слушает служебный inbound: в docker конфигуратор подключается
	// из своей сети, поэтому 0.0.0.0 (доступ закрыт паролем), в systemd — 127.0.0.1.
	SourcesProxyListen string `json:"sources_proxy_listen"`

	// Systemd — параметры установки без контейнеров.
	Systemd SystemdConfig `json:"systemd"`
}

// SystemdConfig — параметры платформы systemd.
type SystemdConfig struct {
	// SingBoxUnit — служба sing-box.
	SingBoxUnit string `json:"sing_box_unit"`

	// SingBoxBinary — бинарник sing-box для sing-box check.
	SingBoxBinary string `json:"sing_box_binary"`

	// ConfigurerUnit — служба конфигуратора (перезапускается при обновлении).
	ConfigurerUnit string `json:"configurer_unit"`

	// ReleaseRepository — репозиторий GitHub, из релизов которого загружаются обновления.
	ReleaseRepository string `json:"release_repository"`
}

// Read читает конфигурацию из файла path. Если файла нет, используются значения по умолчанию.
func Read(path string) (*AppConfig, error) {
	cfg := &AppConfig{
		Platform:            "",
		AppDataPath:         "",
		ListenAddr:          "",
		SourceListsProxyUrl: "",
		SingBoxConfigPath:   "",
		ClashAPIBaseURL:     "",
		ClashAPISecret:      "",
		RuleSetBaseURL:      "",
		BackupDir:           "",
		SourcesProxyPort:    0,
		SourcesProxyListen:  "",
		Systemd: SystemdConfig{
			SingBoxUnit:       "",
			SingBoxBinary:     "",
			ConfigurerUnit:    "",
			ReleaseRepository: "",
		},
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err == nil {
		if err = json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	if cfg.Platform == "" {
		cfg.Platform = detectPlatform()
	}

	if err = cfg.setDefaults(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// setDefaults заполняет незаданные поля значениями по умолчанию для платформы.
func (c *AppConfig) setDefaults() error {
	switch c.Platform {
	case platform.NameDocker:
		setDefault(&c.AppDataPath, "/app/data/app.json")

		// Конфигуратор работает в сети compose, а sing-box — в сети хоста: Clash API доступен
		// через host.docker.internal (extra_hosts: host-gateway в docker-compose.yaml).
		setDefault(&c.ClashAPIBaseURL, "http://host.docker.internal:9090")
		setDefault(&c.SourcesProxyListen, "0.0.0.0")

	case platform.NameSystemd:
		setDefault(&c.AppDataPath, "/var/lib/sing-box-configurer/app.json")
		setDefault(&c.ClashAPIBaseURL, "http://127.0.0.1:9090")
		setDefault(&c.SourcesProxyListen, "127.0.0.1")
		setDefault(&c.Systemd.SingBoxUnit, "sing-box")
		setDefault(&c.Systemd.SingBoxBinary, "/usr/local/bin/sing-box")
		setDefault(&c.Systemd.ConfigurerUnit, "sing-box-configurer")
		setDefault(&c.Systemd.ReleaseRepository, "lanfix/sing-box-configurer")

	default:
		return fmt.Errorf("unknown platform %q (supported: %s, %s)", c.Platform, platform.NameDocker, platform.NameSystemd)
	}

	setDefault(&c.ListenAddr, ":8080")
	setDefault(&c.SingBoxConfigPath, "/etc/sing-box/config.json")
	setDefault(&c.RuleSetBaseURL, "http://127.0.0.1:"+c.ListenPort())
	setDefault(&c.BackupDir, filepath.Join(filepath.Dir(c.AppDataPath), "backups"))

	c.RuleSetBaseURL = strings.TrimSuffix(c.RuleSetBaseURL, "/")

	if c.SourcesProxyPort == 0 {
		c.SourcesProxyPort = 9091
	}

	return nil
}

// ListenPort возвращает порт из адреса listen_addr (8080, если его не удалось разобрать).
func (c *AppConfig) ListenPort() string {
	if _, port, err := net.SplitHostPort(c.ListenAddr); err == nil && port != "" {
		return port
	}

	return "8080"
}

// LocalURL возвращает адрес HTTP-сервера конфигуратора для запросов с этого же хоста.
func (c *AppConfig) LocalURL() string {
	host, _, err := net.SplitHostPort(c.ListenAddr)

	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, c.ListenPort())
}

// detectPlatform определяет платформу: в контейнере Docker есть файл /.dockerenv.
func detectPlatform() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return platform.NameDocker
	}

	return platform.NameSystemd
}

// setDefault задает значение поля, если оно пустое.
func setDefault(field *string, value string) {
	if *field == "" {
		*field = value
	}
}

// SourcesProxyAddr возвращает адрес служебного inbound-а sing-box для конфигуратора: хост Clash API
// (так конфигуратор уже достает до sing-box) и порт sources_proxy_port.
func (c *AppConfig) SourcesProxyAddr() string {
	host := "127.0.0.1"

	if parsed, err := url.Parse(c.ClashAPIBaseURL); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}

	return net.JoinHostPort(host, strconv.Itoa(c.SourcesProxyPort))
}
