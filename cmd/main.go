package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	// Базы часовых поясов встроены в бинарник: в образе их нет, а расписание задается в поясе.
	_ "time/tzdata"

	"github.com/lanfix/sing-box-configurer/cmd/config"
	"github.com/lanfix/sing-box-configurer/internal/amnezia"
	"github.com/lanfix/sing-box-configurer/internal/auth"
	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/handler"
	"github.com/lanfix/sing-box-configurer/internal/happ"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/migrations"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/platform/docker"
	"github.com/lanfix/sing-box-configurer/internal/platform/systemd"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxclashapi"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/scheduler"
	"github.com/lanfix/sing-box-configurer/internal/settings"
	"github.com/lanfix/sing-box-configurer/internal/singbox"
	"github.com/lanfix/sing-box-configurer/internal/trafficmonitor"
	"github.com/lanfix/sing-box-configurer/internal/update"
	"github.com/lanfix/sing-box-configurer/internal/updater"
	"github.com/lanfix/sing-box-configurer/internal/version"
	"github.com/lanfix/sing-box-configurer/view"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file (optional, defaults are used without it)")
	showVersion := flag.Bool("version", false, "Print version and exit")

	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)

		return
	}

	cfg, err := config.Read(*configPath)
	if err != nil {
		log.Fatal(fmt.Errorf("cannot load configuration: %w", err))
	}

	if args := flag.Args(); len(args) > 0 {
		if err = runCommand(cfg, args); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		return
	}

	serve(cfg, *configPath)
}

// usage выводит справку по запуску.
func usage() {
	output := flag.CommandLine.Output()

	_, _ = fmt.Fprintf(output, "Usage:\n")
	_, _ = fmt.Fprintf(output, "  sing-box-configurer [-config path]                     run the service\n")
	_, _ = fmt.Fprintf(output, "  sing-box-configurer [-config path] auth set -username <login> [-password <password>]\n")
	_, _ = fmt.Fprintf(output, "                                                         enable panel login (password is read from stdin if not set)\n")
	_, _ = fmt.Fprintf(output, "  sing-box-configurer [-config path] auth reset          disable panel login\n")
	_, _ = fmt.Fprintf(output, "  sing-box-configurer -version                           print version\n\n")

	flag.PrintDefaults()
}

// serve запускает HTTP-сервер конфигуратора.
func serve(cfg *config.AppConfig, configPath string) {
	log.Printf("Starting Sing-Box Configurer %s (platform: %s, config: %s)", version.Version, cfg.Platform, configPath)

	appData := appdata.NewFile(cfg.AppDataPath)
	singBoxConfigProvider := singboxconfig.NewProvider(cfg.SingBoxConfigPath, cfg.BackupDir)

	if err := os.MkdirAll(filepath.Dir(cfg.AppDataPath), 0755); err != nil {
		log.Fatalf("Cannot create app data dir: %v", err)
	}

	// Миграции выполняются до загрузки данных менеджерами. При ошибке процесс завершается,
	// а updater по отсутствию health-ответа откатывает обновление и восстанавливает бэкап.
	migrationResult, err := migrations.Run(appData, singBoxConfigProvider)
	if err != nil {
		log.Fatalf("Migrations failed: %v", err)
	}

	log.Printf("Data schema version: %d (migrated from %d, applied: %d)",
		migrationResult.ToVersion, migrationResult.FromVersion, len(migrationResult.Applied))

	host, err := newPlatform(cfg, configPath)
	if err != nil {
		log.Fatalf("Cannot initialize platform %s: %v", cfg.Platform, err)
	}

	authManager, err := auth.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize auth manager: %w", err))
	}

	if !authManager.Enabled() {
		log.Printf("Warning: panel login is disabled, anyone with network access can manage sing-box (enable it in System → Settings)")
	}

	settingsManager, err := settings.NewManager(appData)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize settings manager: %w", err))
	}

	// Источники с detour загружаются через служебный inbound sing-box: логин — тег outbound-а.
	detourProxy := func(detour string) (*url.URL, error) {
		return &url.URL{
			Scheme: "http",
			User:   url.UserPassword(detour, settingsManager.Get().SourcesProxy.Password),
			Host:   cfg.SourcesProxyAddr(),
		}, nil
	}

	// Загруженные списки источников хранятся рядом с app.json: после перезапуска rule-set-ы готовы сразу.
	rulesManager, err := rules.NewManager(appData, rules.Options{
		SourceListsProxyURL: cfg.SourceListsProxyUrl,
		CacheDir:            filepath.Join(filepath.Dir(cfg.AppDataPath), "url-sources"),
		DetourProxy:         detourProxy,
	})
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
			SourcesProxy: render.SourcesProxy{
				Listen:  cfg.SourcesProxyListen,
				Port:    cfg.SourcesProxyPort,
				Detours: rulesManager.Detours(),
			},
		}, nil
	}

	singBoxService := singbox.NewService(renderInput, singBoxConfigProvider, host.SingBox, clashAPI)

	// Новая инсталляция: sing-box запустится с конфигом по умолчанию, дальше конфиг применяется из интерфейса.
	if created, err := singBoxService.EnsureConfig(); err != nil {
		log.Printf("Warning: cannot write initial sing-box config: %v", err)
	} else if created {
		log.Printf("Initial sing-box config written to %s", cfg.SingBoxConfigPath)
	}

	// Плановая перезагрузка sing-box.
	restartTask := scheduler.NewRestartTask(func() settings.Restart {
		return settingsManager.Get().Restart
	}, func() error {
		return singBoxService.Restart(context.Background())
	})
	restartTask.Start(context.Background())

	h := handler.NewHandler(handler.Deps{
		Auth:           authManager,
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
		Update:         update.NewService(host),
	})

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", handler.Health(migrationResult, host.Name))
	h.Register(mux)

	// Неизвестные методы API — 404, а не страница интерфейса.
	mux.HandleFunc("/api/", http.NotFound)
	mux.Handle("/", view.Handler())

	log.Printf("Server started on %s (rule-sets for sing-box: %s)", cfg.ListenAddr, cfg.RuleSetBaseURL)

	// Таймауты не дают медленным клиентам держать соединения бесконечно (slowloris). WriteTimeout не задан:
	// замер задержек группы и трассировка могут отвечать долго.
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler.Protect(authManager.Middleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	if err = server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// newPlatform создает платформу установки из конфигурации.
func newPlatform(cfg *config.AppConfig, configPath string) (platform.Platform, error) {
	switch cfg.Platform {
	case platform.NameDocker:
		return docker.New(docker.Options{
			ListenPort: cfg.ListenPort(),
		})

	case platform.NameSystemd:
		appDataPath, err := filepath.Abs(cfg.AppDataPath)
		if err != nil {
			return platform.Platform{}, err
		}

		// Перед обновлением сохраняются данные приложения, конфиг сервиса и рабочий конфиг sing-box.
		backupPaths := []string{appDataPath}

		for _, path := range []string{configPath, cfg.SingBoxConfigPath} {
			if abs, err := filepath.Abs(path); err == nil {
				backupPaths = append(backupPaths, abs)
			}
		}

		return systemd.New(systemd.Options{
			SingBoxUnit:    cfg.Systemd.SingBoxUnit,
			SingBoxBinary:  cfg.Systemd.SingBoxBinary,
			ConfigurerUnit: cfg.Systemd.ConfigurerUnit,
			HealthURL:      cfg.LocalURL() + "/api/health",
			UpdatesDir:     filepath.Join(filepath.Dir(appDataPath), updater.UpdatesDirName),
			BackupPaths:    backupPaths,
			Repository:     cfg.Systemd.ReleaseRepository,
		}), nil
	}

	return platform.Platform{}, fmt.Errorf("unknown platform %q", cfg.Platform)
}

// outboundConfigs возвращает объекты sing-box outbound-ов, добавленных вручную.
func outboundConfigs(items []outbound.Item) []map[string]any {
	configs := make([]map[string]any, 0, len(items))

	for _, item := range items {
		configs = append(configs, item.Config)
	}

	return configs
}
