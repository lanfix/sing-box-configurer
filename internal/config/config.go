package config

import (
	"encoding/json"
	"flag"
	"os"
)

// AppConfig represents the application configuration
type AppConfig struct {
	RulesPath           string `json:"rules_path"`
	ListenAddr          string `json:"listen_addr"`
	DockerControllerURL string `json:"docker_controller_url"`
	SourceListsProxyUrl string `json:"source_lists_proxy_url"`
}

func LoadAppConfig(configPath string) (*AppConfig, error) {
	config := &AppConfig{
		RulesPath:           "rules.json",
		ListenAddr:          ":8080",
		DockerControllerURL: "http://127.0.0.1:8081",
		SourceListsProxyUrl: "",
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			data, _ := json.MarshalIndent(config, "", "  ")
			os.WriteFile(configPath, data, 0644)

			return config, nil
		}

		return nil, err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

func ParseFlags() (string, *AppConfig) {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	rulesPath := flag.String("rules", "", "Path to rules file (overrides config)")
	listenAddr := flag.String("listen", "", "Listen address (overrides config)")

	flag.Parse()

	// Load configuration from file
	config, err := LoadAppConfig(*configPath)
	if err != nil {
		config = &AppConfig{
			RulesPath:           "rules.json",
			ListenAddr:          ":8080",
			DockerControllerURL: "http://127.0.0.1:8081",
			SourceListsProxyUrl: "",
		}
	}

	// Override with command-line flags
	if *rulesPath != "" {
		config.RulesPath = *rulesPath
	}
	if *listenAddr != "" {
		config.ListenAddr = *listenAddr
	}

	return *configPath, config
}
