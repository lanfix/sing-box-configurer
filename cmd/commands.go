package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lanfix/sing-box-configurer/cmd/config"
	"github.com/lanfix/sing-box-configurer/internal/auth"
	"github.com/lanfix/sing-box-configurer/internal/migrations"
	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// runCommand выполняет служебную команду (например, auth reset) вместо запуска сервиса.
func runCommand(cfg *config.AppConfig, args []string) error {
	if (args[0] != "auth" && args[0] != "security") || len(args) < 2 {
		flag.Usage()

		return fmt.Errorf("unknown command %q", strings.Join(args, " "))
	}

	appData := appdata.NewFile(cfg.AppDataPath)

	if err := os.MkdirAll(filepath.Dir(cfg.AppDataPath), 0755); err != nil {
		return fmt.Errorf("cannot create app data dir: %w", err)
	}

	// Команда может выполняться до первого запуска сервиса: миграции записывают версию схемы новой
	// инсталляции, иначе сервис примет app.json с одним разделом auth за данные старой версии.
	if _, err := migrations.Run(appData, singboxconfig.NewProvider(cfg.SingBoxConfigPath, cfg.BackupDir)); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	if args[0] == "security" {
		return securityCommand(cfg, appData, args[1:])
	}

	manager, err := auth.NewManager(appData)
	if err != nil && args[1] != "reset" {
		return err
	}

	// Поврежденные учетные данные не мешают сбросу: раздел auth перезаписывается целиком.
	if err != nil {
		manager, err = authManagerForReset(appData)
		if err != nil {
			return err
		}
	}

	switch args[1] {
	case "set":
		if err = authSet(manager, args[2:]); err != nil {
			return err
		}

		fmt.Println("Panel login enabled.")

	case "reset":
		if err = manager.Reset(); err != nil {
			return err
		}

		fmt.Println("Panel login disabled, the panel is open without authentication.")

	default:
		flag.Usage()

		return fmt.Errorf("unknown command %q", strings.Join(args, " "))
	}

	fmt.Println("Restart the service to apply: " + restartHint(cfg))

	return nil
}

// authSet разбирает флаги команды auth set и включает вход.
func authSet(manager *auth.Manager, args []string) error {
	flags := flag.NewFlagSet("auth set", flag.ContinueOnError)
	username := flags.String("username", "", "Login")
	password := flags.String("password", "", "Password (read from stdin if not set)")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *username == "" {
		return errors.New("-username is required")
	}

	if *password == "" {
		fmt.Print("Password: ")

		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("cannot read password: %w", err)
		}

		*password = strings.TrimRight(line, "\r\n")
	}

	return manager.Set(*username, *password)
}

// authManagerForReset создает менеджер поверх поврежденного раздела auth: сначала раздел очищается.
func authManagerForReset(appData *appdata.File) (*auth.Manager, error) {
	section := map[string]any{
		"auth": nil,
	}

	if err := appData.Merge(section); err != nil {
		return nil, err
	}

	return auth.NewManager(appData)
}

// restartHint возвращает команду перезапуска сервиса для платформы.
func restartHint(cfg *config.AppConfig) string {
	if cfg.Platform == platform.NameDocker {
		return "docker restart sing-box-configurer"
	}

	return "systemctl restart " + cfg.Systemd.ConfigurerUnit
}

// securityCommand выполняет команду security reset: выключает проверку адреса панели, если панель
// перестала открываться по домену.
func securityCommand(cfg *config.AppConfig, appData *appdata.File, args []string) error {
	if len(args) != 1 || args[0] != "reset" {
		flag.Usage()

		return fmt.Errorf("unknown command %q", "security "+strings.Join(args, " "))
	}

	manager, err := settings.NewManager(appData)
	if err != nil {
		return err
	}

	security := manager.Get().Security
	security.CheckHost = false

	if err = manager.UpdateSecurity(security); err != nil {
		return err
	}

	fmt.Println("Panel host check disabled, the panel opens by any domain name.")
	fmt.Println("Restart the service to apply: " + restartHint(cfg))

	return nil
}
