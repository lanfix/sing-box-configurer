package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/config"
	"github.com/lanfix/sing-box-configurer/internal/dockercontroller"
	"github.com/lanfix/sing-box-configurer/internal/handler"
	"github.com/lanfix/sing-box-configurer/internal/rules"
	"github.com/lanfix/sing-box-configurer/internal/singbox"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	configPath, appConfig := config.ParseFlags()

	log.Printf("Starting Sing-Box Configurer")
	log.Printf("Config file: %s", configPath)
	log.Printf("Rules file: %s", appConfig.RulesPath)
	log.Printf("Listen address: %s", appConfig.ListenAddr)

	rulesManager, err := rules.NewManager(appConfig.RulesPath, appConfig.SourceListsProxyUrl)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize rules manager: %w", err))
	}

	if err := rulesManager.Load(); err != nil {
		log.Printf("Warning: could not load rules: %v", err)
	}

	rulesManager.StartAllURLSourceUpdates()

	dockerControllerAPI := dockercontroller.NewAPI(appConfig.DockerControllerURL)

	configManager := singbox.NewConfigManager(appConfig.SingBoxConfigPath)

	h := handler.NewHandler(rulesManager, dockerControllerAPI, configManager)

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
	http.HandleFunc("/api/rules/delete", h.DeleteRule)
	http.HandleFunc("/api/apply", h.ApplyRules)
	http.HandleFunc("/api/ruleset", h.GetRuleSet)

	http.HandleFunc("/api/url-sources", h.GetURLSources)
	http.HandleFunc("/api/url-sources/add", h.AddURLSource)
	http.HandleFunc("/api/url-sources/delete", h.DeleteURLSource)
	http.HandleFunc("/api/url-sources/apply", h.ApplyURLSources)
	http.HandleFunc("/api/url-sources/validate", h.ValidateURLSource)
	http.HandleFunc("/api/url-sources/rules", h.GetURLSourceRules)

	http.HandleFunc("/api/control/reload", h.ReloadSingBox)

	http.HandleFunc("/api/config/get", h.GetSingBoxConfig)
	http.HandleFunc("/api/config/save-temp", h.SaveTempConfig)
	http.HandleFunc("/api/config/apply", h.ApplySingBoxConfig)
	http.HandleFunc("/api/config/discard", h.DiscardTempConfig)

	log.Printf("Server started on %s", appConfig.ListenAddr)

	if err := http.ListenAndServe(appConfig.ListenAddr, nil); err != nil {
		log.Fatal(err)
	}
}
