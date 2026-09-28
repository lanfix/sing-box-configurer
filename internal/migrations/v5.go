package migrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/dnsconfig"
	"github.com/lanfix/sing-box-configurer/internal/inbounds"
	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/migrations/legacy"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/settings"
)

// Имена системных групп на момент миграции 5.
const (
	v5BlockGroup  = "block"
	v5BypassGroup = "bypass"
)

// migrateConfigToAppData переносит в app.json все, что раньше настраивалось только в конфиге sing-box:
// DNS-серверы, общие параметры и пользовательские правила DNS, outbound-ы, добавленные вручную,
// mixed-inbound-ы, уровень логов и доступ к Clash API. После миграции конфиг sing-box рендерится
// из app.json. Сам конфиг sing-box не меняется: новый рендер применяется со страницы «Конфиг».
//
// Флаг «Мимо туннеля» у правил и URL-источников заменяется системной группой bypass, а группа block
// становится системной (ее правила отклоняют соединения).
func migrateConfigToAppData(state *State) error {
	if err := migrateBypassFlag(state, "rules"); err != nil {
		return err
	}

	if err := migrateBypassFlag(state, "url_sources"); err != nil {
		return err
	}

	if err := dropSystemGroups(state); err != nil {
		return err
	}

	config, err := state.SingBoxConfig()
	if errors.Is(err, os.ErrNotExist) {
		// Конфига еще нет — менеджеры создадут настройки по умолчанию.
		log.Printf("Migration 5: sing-box config not found, defaults will be used")

		return nil
	}

	if err != nil {
		return err
	}

	syncedTags, err := subscriptionTags(state)
	if err != nil {
		return err
	}

	steps := []func(map[string]any, *State, []string) error{
		importDNS,
		importOutbounds,
		importInbounds,
		importSettings,
	}

	for _, step := range steps {
		if err = step(config, state, syncedTags); err != nil {
			return err
		}
	}

	return nil
}

// migrateBypassFlag переносит правила или источники раздела key с флагом bypass в группу bypass.
func migrateBypassFlag(state *State, key string) error {
	raw, ok := state.AppData[key]
	if !ok {
		return nil
	}

	var items []map[string]any

	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("cannot parse %s: %w", key, err)
	}

	for _, item := range items {
		if bypass, _ := item["bypass"].(bool); bypass {
			item["group"] = v5BypassGroup
		}

		delete(item, "bypass")
	}

	return setAppData(state, key, items)
}

// dropSystemGroups удаляет из пользовательских групп block и bypass: теперь это системные группы.
func dropSystemGroups(state *State) error {
	raw, ok := state.AppData["groups"]
	if !ok {
		return nil
	}

	var groups []map[string]any

	if err := json.Unmarshal(raw, &groups); err != nil {
		return fmt.Errorf("cannot parse groups: %w", err)
	}

	groups = slices.DeleteFunc(groups, func(group map[string]any) bool {
		name, _ := group["name"].(string)

		return name == v5BlockGroup || name == v5BypassGroup
	})

	return setAppData(state, "groups", groups)
}

// subscriptionTags возвращает теги outbound-ов, записанных в конфиг профилями Happ и Amnezia.
func subscriptionTags(state *State) ([]string, error) {
	var section struct {
		Profiles []struct {
			SyncedTags []string `json:"synced_tags"`
		} `json:"profiles"`
	}

	tags := make([]string, 0)

	for _, key := range []string{"happ", "amnezia"} {
		raw, ok := state.AppData[key]
		if !ok {
			continue
		}

		if err := json.Unmarshal(raw, &section); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", key, err)
		}

		for _, profile := range section.Profiles {
			tags = append(tags, profile.SyncedTags...)
		}
	}

	return tags, nil
}

// importDNS переносит DNS-серверы, общие параметры и пользовательские DNS-правила.
func importDNS(config map[string]any, state *State, _ []string) error {
	dns, _ := config["dns"].(map[string]any)
	route, _ := config["route"].(map[string]any)

	data := dnsconfig.Data{
		Servers: []dnsconfig.Server{},
		Settings: dnsconfig.Settings{
			Final:                 jsonmap.String(dns, "final"),
			Strategy:              jsonmap.String(dns, "strategy"),
			DefaultDomainResolver: "",
			CacheCapacity:         jsonmap.Int(dns, "cache_capacity"),
			Optimistic:            jsonmap.Bool(dns, "optimistic"),
			Timeout:               jsonmap.String(dns, "timeout"),
			Extra:                 jsonmap.Without(dns, "servers", "rules", "final", "strategy", "cache_capacity", "optimistic", "timeout", "reverse_mapping"),
		},
		Rules: []map[string]any{},
	}

	if len(data.Settings.Extra) == 0 {
		data.Settings.Extra = nil
	}

	// default_domain_resolver задается строкой или объектом {"server": ...}.
	switch resolver := route["default_domain_resolver"].(type) {
	case string:
		data.Settings.DefaultDomainResolver = resolver

	case map[string]any:
		data.Settings.DefaultDomainResolver = jsonmap.String(resolver, "server")
	}

	servers, _ := dns["servers"].([]any)

	for _, item := range servers {
		server, ok := item.(map[string]any)
		if !ok || legacy.IsHostsServer(item) {
			continue
		}

		data.Servers = append(data.Servers, dnsconfig.ServerFromConfig(server))
	}

	rules, _ := dns["rules"].([]any)

	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok || legacy.IsHostsRule(item) || legacy.IsHTTPSFilterRule(rule) {
			continue
		}

		if _, _, system := legacy.ParseSystemDNSRule(rule); system {
			continue
		}

		data.Rules = append(data.Rules, rule)
	}

	log.Printf("Migration 5: imported %d dns servers and %d custom dns rules", len(data.Servers), len(data.Rules))

	return setAppData(state, "dns", data)
}

// importOutbounds переносит outbound-ы и endpoint-ы, добавленные вручную. Системные selector-ы групп,
// встроенные outbound-ы и серверы подписок пропускаются: их создает рендер.
func importOutbounds(config map[string]any, state *State, syncedTags []string) error {
	items := make([]outbound.Item, 0)

	for _, section := range []string{"outbounds", "endpoints"} {
		list, _ := config[section].([]any)

		for _, entry := range list {
			item, ok := entry.(map[string]any)
			if !ok {
				continue
			}

			tag := jsonmap.String(item, "tag")
			itemType := jsonmap.String(item, "type")

			if slices.Contains(syncedTags, tag) || itemType == "direct" || itemType == "block" || itemType == "dns" {
				continue
			}

			// auto стал встроенным urltest, selector-ы групп создает рендер.
			if tag == outbound.AutoTag || tag == legacy.SelectorTagPrefix || strings.HasPrefix(tag, outbound.SelectorTagPrefix) {
				continue
			}

			items = append(items, outbound.Item{
				ID:        uuid.NewString(),
				Config:    item,
				CreatedAt: time.Now(),
			})
		}
	}

	log.Printf("Migration 5: imported %d manual outbounds", len(items))

	return setAppData(state, "outbounds", items)
}

// importInbounds переносит mixed-inbound-ы. tun и dns inbound-ы встроены в рендер.
func importInbounds(config map[string]any, state *State, _ []string) error {
	data := inbounds.Data{
		Mixed: []inbounds.Mixed{},
	}

	list, _ := config["inbounds"].([]any)

	for _, entry := range list {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		switch jsonmap.String(item, "type") {
		case "mixed":
			data.Mixed = append(data.Mixed, inbounds.MixedFromConfig(item))

		case "tun", "direct":
			continue

		default:
			log.Printf("Migration 5: inbound %s of type %s is not supported and will be dropped", jsonmap.String(item, "tag"), jsonmap.String(item, "type"))
		}
	}

	return setAppData(state, "inbounds", data)
}

// v5Settings — формат раздела settings на момент миграции 5 (без плановой перезагрузки, ее добавляет
// менеджер настроек при загрузке).
type v5Settings struct {
	LogLevel string            `json:"log_level"`
	ClashAPI settings.ClashAPI `json:"clash_api"`
}

// importSettings переносит уровень логов и CORS Clash API. Секрет берется из конфига, если он задан.
func importSettings(config map[string]any, state *State, _ []string) error {
	data := v5Settings{
		LogLevel: "warn",
		ClashAPI: settings.ClashAPI{
			Secret:       "",
			AllowOrigins: []string{"*"},
		},
	}

	logSection, _ := config["log"].(map[string]any)

	if level := jsonmap.String(logSection, "level"); level != "" {
		data.LogLevel = level
	}

	experimental, _ := config["experimental"].(map[string]any)
	clashAPI, _ := experimental["clash_api"].(map[string]any)

	if origins := jsonmap.Strings(clashAPI, "access_control_allow_origin"); origins != nil {
		data.ClashAPI.AllowOrigins = origins
	}

	data.ClashAPI.Secret = jsonmap.String(clashAPI, "secret")

	if data.ClashAPI.Secret == "" {
		secret, err := settings.GenerateSecret()
		if err != nil {
			return err
		}

		data.ClashAPI.Secret = secret
	}

	return setAppData(state, "settings", data)
}

// setAppData записывает значение раздела key в app.json.
func setAppData(state *State, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot marshal %s: %w", key, err)
	}

	state.AppData[key] = raw

	return nil
}
