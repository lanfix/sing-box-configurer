package migrations

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/dnsrecords"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

// registry — список всех миграций. Миграции никогда не удаляются и не меняются после релиза,
// новые добавляются в конец со следующим номером версии.
//
// Миграция должна:
//   - менять только state.AppData и/или конфиг из state.SingBoxConfig();
//   - вызывать state.MarkSingBoxConfigChanged(), если изменила конфиг sing-box;
//   - возвращать ошибку, если данные нельзя привести к новому формату (обновление откатится).
var registry = []Migration{
	{
		Version: 1,
		Name:    "introduce schema_version",
		Up: func(_ *State) error {
			// Данные до появления миграций уже в актуальном формате, фиксируем только версию схемы.
			return nil
		},
	},
	{
		Version: 2,
		Name:    "add tunnel bypass rule-set",
		Up: func(state *State) error {
			config, err := state.SingBoxConfig()
			if err != nil {
				return err
			}

			if err = singboxconfig.EnsureBypass(config); err != nil {
				return fmt.Errorf("cannot add bypass to sing-box config: %w", err)
			}

			state.MarkSingBoxConfigChanged()

			return nil
		},
	},
	{
		Version: 3,
		Name:    "move group dns servers from manual dns rules",
		Up:      migrateGroupDNSServers,
	},
	{
		Version: 4,
		Name:    "move hosts dns servers to dns records",
		Up:      migrateHostsToDNSRecords,
	},
}

// migrateHostsToDNSRecords переносит записи пользовательских hosts-серверов (predefined) в DNS-записи
// конфигуратора. В конфиге sing-box такие серверы и их правила заменяются системными, ответы DNS не меняются.
func migrateHostsToDNSRecords(state *State) error {
	config, err := state.SingBoxConfig()
	if err != nil {
		return err
	}

	imported, err := singboxconfig.ImportHostsServers(config)
	if err != nil {
		return fmt.Errorf("cannot import hosts servers: %w", err)
	}

	if len(imported) == 0 {
		return nil
	}

	var records []dnsrecords.Record

	if raw, ok := state.AppData["dns_records"]; ok {
		if err = json.Unmarshal(raw, &records); err != nil {
			return fmt.Errorf("cannot parse dns records: %w", err)
		}
	}

	for _, record := range imported {
		if slices.ContainsFunc(records, func(existing dnsrecords.Record) bool {
			return existing.Domain == record.Domain
		}) {
			continue
		}

		records = append(records, dnsrecords.Record{
			ID:          uuid.NewString(),
			Domain:      record.Domain,
			Addresses:   record.Addresses,
			Description: "Перенесено из hosts-сервера конфига sing-box",
			CreatedAt:   time.Now(),
		})
	}

	raw, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("cannot marshal dns records: %w", err)
	}

	state.AppData["dns_records"] = raw

	if err = singboxconfig.SyncDNSRecords(config, dnsrecords.ToConfigRecords(records)); err != nil {
		return fmt.Errorf("cannot sync dns records: %w", err)
	}

	state.MarkSingBoxConfigChanged()

	return nil
}

// migrateGroupDNSServers переносит DNS-серверы групп из вручную заданных DNS-правил вида
// {"rule_set": "configurer-<group>", "server": "..."} в настройки групп. Конфиг sing-box не меняется:
// новые DNS-правила и разделенные rule-set-ы появятся после синхронизации групп (через diff в редакторе).
func migrateGroupDNSServers(state *State) error {
	raw, ok := state.AppData["groups"]
	if !ok {
		return nil
	}

	var groups []map[string]any

	if err := json.Unmarshal(raw, &groups); err != nil {
		return fmt.Errorf("cannot parse groups: %w", err)
	}

	config, err := state.SingBoxConfig()
	if err != nil {
		return err
	}

	servers := singboxconfig.GetGroupDNSServers(config)
	changed := false

	for _, group := range groups {
		name, _ := group["name"].(string)
		server, ok := servers[name]

		if !ok {
			continue
		}

		if current, _ := group["dns_server"].(string); current != "" {
			continue
		}

		group["dns_server"] = server
		changed = true
	}

	if !changed {
		return nil
	}

	raw, err = json.Marshal(groups)
	if err != nil {
		return fmt.Errorf("cannot marshal groups: %w", err)
	}

	state.AppData["groups"] = raw

	return nil
}
