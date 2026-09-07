package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/lanfix/sing-box-configurer/cmd/config"
	"github.com/lanfix/sing-box-configurer/internal/handler"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/repository/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
	"github.com/lanfix/sing-box-configurer/internal/rules"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Read[config.AppConfig](*configPath)
	if err != nil {
		log.Fatal(fmt.Errorf("cannot load configuration: %w", err))
	}

	log.Printf("Starting Sing-Box Configurer")
	log.Printf("Config [%s]: %+v", *configPath, cfg)

	if cfg.AppDataPath == "" {
		log.Fatalf("Field app_data_path required in config")
	}

	rulesManager, err := rules.NewManager(cfg.AppDataPath, cfg.SourceListsProxyUrl)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize rules manager: %w", err))
	}

	singBoxConfigProvider := singboxconfig.NewProvider(cfg.SingBoxConfigPath)

	migrationPerformed := false

	if err := rulesManager.Load(); err != nil {
		log.Printf("Warning: could not load rules: %v", err)
	} else {
		// Проверяем, была ли выполнена миграция.
		migrationPerformed = rulesManager.WasMigrated()
	}

	// Если была выполнена миграция, синхронизируем группы в конфиг sing-box.
	if migrationPerformed {
		log.Println("Syncing groups to sing-box config after migration...")

		groups := rulesManager.GetGroups()
		var configGroups []singboxconfig.Group

		for _, g := range groups {
			configGroups = append(configGroups, singboxconfig.Group{
				Name:            g.Name,
				Description:     g.Description,
				DefaultOutbound: g.DefaultOutbound,
			})
		}

		if err := singBoxConfigProvider.SyncGroupsToConfig(cfg.SingBoxConfigPath, configGroups); err != nil {
			log.Printf("Warning: failed to sync groups to config after migration: %v", err)
		} else {
			log.Println("Successfully synced groups to sing-box config")
		}
	}

	rulesManager.StartAllURLSourceUpdates()

	dockerControllerProvider := dockercontroller.NewProvider(cfg.DockerControllerURL)

	outboundManager := outbound.NewManager(singBoxConfigProvider)

	h := handler.NewHandler(rulesManager, dockerControllerProvider, singBoxConfigProvider, outboundManager)

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)

			return
		}

		data, err := staticFS.Open("index.html")
		if err != nil {
			http.Error(w, "Could not open index.html", http.StatusInternalServerError)

			return
		}

		defer func() {
			_ = data.Close()
		}()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, data.(io.ReadSeeker))
	})

	http.HandleFunc("/api/rules", h.GetRules)
	http.HandleFunc("/api/rules/add", h.AddRule)
	http.HandleFunc("/api/rules/edit", h.EditRule)
	http.HandleFunc("/api/rules/delete", h.DeleteRule)
	http.HandleFunc("/api/apply", h.ApplyRules)
	http.HandleFunc("/api/ruleset", h.GetRuleSet)
	http.HandleFunc("/api/ruleset/group", h.GetRuleSetByGroup)

	http.HandleFunc("/api/groups", h.GetGroups)
	http.HandleFunc("/api/groups/add", h.AddGroup)
	http.HandleFunc("/api/groups/edit", h.EditGroup)
	http.HandleFunc("/api/groups/delete", h.DeleteGroup)

	http.HandleFunc("/api/url-sources", h.GetURLSources)
	http.HandleFunc("/api/url-sources/add", h.AddURLSource)
	http.HandleFunc("/api/url-sources/edit", h.EditURLSource)
	http.HandleFunc("/api/url-sources/delete", h.DeleteURLSource)
	http.HandleFunc("/api/url-sources/apply", h.ApplyURLSources)
	http.HandleFunc("/api/url-sources/validate", h.ValidateURLSource)
	http.HandleFunc("/api/url-sources/rules", h.GetURLSourceRules)

	http.HandleFunc("/api/control/reload", h.ReloadSingBox)

	http.HandleFunc("/api/config/get", h.GetSingBoxConfig)
	http.HandleFunc("/api/config/save-temp", h.SaveTempConfig)
	http.HandleFunc("/api/config/apply", h.ApplySingBoxConfig)
	http.HandleFunc("/api/config/discard", h.DiscardTempConfig)
	http.HandleFunc("/api/config/check-pending", h.CheckPendingConfig)
	http.HandleFunc("/api/config/sync-groups", h.SyncGroups)
	http.HandleFunc("/api/config/check-groups-sync", h.CheckGroupsSync)

	http.HandleFunc("/api/outbounds", h.GetOutbounds)
	http.HandleFunc("/api/outbounds/add", h.AddOutbound)
	http.HandleFunc("/api/outbounds/delete", h.DeleteOutbound)

	log.Printf("Server started on %s", cfg.ListenAddr)

	if err := http.ListenAndServe(cfg.ListenAddr, nil); err != nil {
		log.Fatal(err)
	}
}
