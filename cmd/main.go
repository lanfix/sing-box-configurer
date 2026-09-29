package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	// Базы часовых поясов встроены в бинарник: в образе debian-slim их нет, а расписание задается в поясе.
	_ "time/tzdata"

	"github.com/lanfix/sing-box-configurer/cmd/config"
	"github.com/lanfix/sing-box-configurer/internal/amnezia"
	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/handler"
	"github.com/lanfix/sing-box-configurer/internal/happ"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/migrations"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/scheduler"
	"github.com/lanfix/sing-box-configurer/internal/settings"
	"github.com/lanfix/sing-box-configurer/internal/singbox"
	"github.com/lanfix/sing-box-configurer/internal/trafficmonitor"
	"github.com/lanfix/sing-box-configurer/internal/update"
	"github.com/lanfix/sing-box-configurer/internal/version"
	"github.com/lanfix/sing-box-configurer/view"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Read(*configPath)
	if err != nil {
		log.Fatal(fmt.Errorf("cannot load configuration: %w", err))
	}

	log.Printf("Starting Sing-Box Configurer %s (config: %s)", version.Version, *configPath)

	if cfg.AppDataPath == "" {
		log.Fatalf("Field app_data_path required in config")
	}

	appData := appdata.NewFile(cfg.AppDataPath)
	singBoxConfigProvider := singboxconfig.NewProvider(cfg.SingBoxConfigPath, cfg.BackupDir)

	// Миграции выполняются до загрузки данных менеджерами. При ошибке процесс завершается,
	// а updater по отсутствию health-ответа откатывает обновление и восстанавливает бэкап.
	migrationResult, err := migrations.Run(appData, singBoxConfigProvider)
	if err != nil {
		log.Fatalf("Migrations failed: %v", err)
	}

	log.Printf("Data schema version: %d (migrated from %d, applied: %d)",
		migrationResult.ToVersion, migrationResult.FromVersion, len(migrationResult.Applied))

	rulesManager, err := rules.NewManager(appData, cfg.SourceListsProxyUrl)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize rules manager: %w", err))
	}

	if err = rulesManager.Load(); err != nil {
		log.Printf("Warning: could not load rules: %v", err)
	}

	rulesManager.StartAllURLSourceUpdates()

	dnsManager, err := dnsconfig.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize dns manager: %w", err))
	}

	dnsRecordsManager, err := dnsrecords.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize dns records manager: %w", err))
	}

	outboundManager, err := outbound.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize outbound manager: %w", err))
	}

	inboundsManager, err := inbounds.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize inbounds manager: %w", err))
	}

	settingsManager, err := settings.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize settings manager: %w", err))
	}

	dockerControllerProvider := dockercontroller.NewProvider(cfg.DockerControllerURL, cfg.DockerControllerAPIKey)

	// Секрет Clash API берется из рабочего конфига sing-box: он меняется только при применении конфига.
	clashAPI := singboxclashapi.NewClashAPI(cfg.ClashAPIBaseURL, func() string {
		if secret := singBoxConfigProvider.ClashSecret(); secret != "" {
			return secret
		}

		return cfg.ClashAPISecret
	})

	// Окно на 60 измерений — Clash API отдаёт скорость раз в секунду.
	trafficMonitor := trafficmonitor.New(clashAPI, 60)
	trafficMonitor.Start(context.Background())

	happStore, err := happ.NewStore(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize happ store: %w", err))
	}

	log.Printf("Happ installation id: %s", happStore.InstallationID())

	happManager := happ.NewManager(happStore, happ.NewClient())
	happManager.Start(context.Background())

	amneziaGateway, err := amnezia.NewGatewayClient(amnezia.DefaultGatewayURL)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize amnezia gateway client: %w", err))
	}

	amneziaManager, err := amnezia.NewManager(appData, clashAPI, amneziaGateway)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize amnezia manager: %w", err))
	}

	amneziaManager.Start(context.Background())

	// Данные для рендера конфига sing-box собираются из менеджеров при каждом рендере.
	renderInput := func() (render.Input, error) {
		amneziaSubscriptions, amneziaWarnings := amneziaManager.Subscriptions()

		for _, warning := range amneziaWarnings {
			log.Printf("Render: %s", warning)
		}

		return render.Input{
			Groups:         rulesManager.GetGroups(),
			DNS:            dnsManager.Get(),
			DNSRecords:     dnsRecordsManager.List(),
			Outbounds:      outboundConfigs(outboundManager.List()),
			Subscriptions:  append(happManager.Subscriptions(), amneziaSubscriptions...),
			URLTests:       outboundManager.ListURLTests(),
			Mixed:          inboundsManager.Mixed(),
			Settings:       settingsManager.Get(),
			RuleSetBaseURL: cfg.RuleSetBaseURL,
		}, nil
	}

	singBoxService := singbox.NewService(renderInput, singBoxConfigProvider, dockerControllerProvider, clashAPI)

	// Плановая перезагрузка sing-box (раньше ее выполнял отдельный контейнер cron-scheduler).
	restartTask := scheduler.NewRestartTask(func() settings.Restart {
		return settingsManager.Get().Restart
	}, singBoxService.Restart)
	restartTask.Start(context.Background())

	updateService := update.NewService(dockerControllerProvider, update.NewRegistry(), cfg.ListenAddr)

	h := handler.NewHandler(handler.Deps{
		Rules:          rulesManager,
		DNS:            dnsManager,
		DNSRecords:     dnsRecordsManager,
		Outbounds:      outboundManager,
		Inbounds:       inboundsManager,
		Settings:       settingsManager,
		RestartTask:    restartTask,
		Happ:           happManager,
		Amnezia:        amneziaManager,
		SingBox:        singBoxService,
		ClashAPI:       clashAPI,
		TrafficMonitor: trafficMonitor,
		Update:         updateService,
	})

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", handler.Health(migrationResult))
	h.Register(mux)

	// Неизвестные методы API — 404, а не страница интерфейса.
	mux.HandleFunc("/api/", http.NotFound)
	mux.Handle("/", view.Handler())

	log.Printf("Server started on %s (rule-sets for sing-box: %s)", cfg.ListenAddr, cfg.RuleSetBaseURL)

	if err = http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

// outboundConfigs возвращает объекты sing-box outbound-ов, добавленных вручную.
func outboundConfigs(items []outbound.Item) []map[string]any {
	configs := make([]map[string]any, 0, len(items))

	for _, item := range items {
		configs = append(configs, item.Config)
	}

	return configs
}
