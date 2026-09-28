package migrations

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

var (
	// nestedTransportKeys — ключи, под которые старый парсер share-ссылок вкладывал параметры транспорта.
	nestedTransportKeys = []string{"ws", "grpc", "xhttp", "http", "httpupgrade"}

	// dashPortRangeRe — диапазон портов через дефис, как в share-ссылке hysteria2.
	dashPortRangeRe = regexp.MustCompile(`^\d+-\d+$`)
)

// migrateFixShareOutbounds исправляет outbound-ы, добавленные вручную из share-ссылок старым парсером:
//   - параметры транспорта были вложены в transport.ws / transport.grpc / transport.xhttp, а sing-box
//     ждет их на одном уровне с type (параметр mode у xhttp в httpupgrade не переносится);
//   - диапазон портов hysteria2 был записан через дефис, а sing-box ждет двоеточие.
func migrateFixShareOutbounds(state *State) error {
	raw, ok := state.AppData["outbounds"]
	if !ok {
		return nil
	}

	var items []map[string]any

	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("cannot parse outbounds: %w", err)
	}

	fixed := 0

	for _, item := range items {
		config, _ := item["config"].(map[string]any)

		if flattenTransport(config) {
			fixed++
		}

		if fixPortRanges(config) {
			fixed++
		}
	}

	if fixed == 0 {
		return nil
	}

	log.Printf("Migration 6: fixed %d share link outbound fields", fixed)

	return setAppData(state, "outbounds", items)
}

// flattenTransport переносит вложенные параметры транспорта на один уровень с type.
func flattenTransport(config map[string]any) bool {
	transport, _ := config["transport"].(map[string]any)
	if transport == nil {
		return false
	}

	changed := false

	for _, key := range nestedTransportKeys {
		nested, ok := transport[key].(map[string]any)
		if !ok {
			continue
		}

		for field, value := range nested {
			if field == "mode" {
				continue
			}

			if _, exists := transport[field]; !exists {
				transport[field] = value
			}
		}

		delete(transport, key)
		changed = true
	}

	return changed
}

// fixPortRanges заменяет дефис на двоеточие в диапазонах server_ports.
func fixPortRanges(config map[string]any) bool {
	ports, _ := config["server_ports"].([]any)
	changed := false

	for i, item := range ports {
		value, ok := item.(string)
		if !ok || !dashPortRangeRe.MatchString(value) {
			continue
		}

		ports[i] = strings.Replace(value, "-", ":", 1)
		changed = true
	}

	return changed
}
