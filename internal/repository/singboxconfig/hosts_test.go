package singboxconfig

import (
	"reflect"
	"slices"
	"testing"
)

// staticHostsConfig — конфиг с hosts-сервером, заданным вручную.
const staticHostsConfig = `{
	"dns": {
		"final": "yandex",
		"servers": [
			{"type": "udp", "tag": "yandex", "server": "77.88.8.1"},
			{"type": "hosts", "tag": "static-hosts", "predefined": {
				"ha.home.lab": ["192.168.50.8"],
				"truenas.home.lab": "192.168.50.19"
			}},
			{"type": "hosts", "tag": "file-hosts", "path": "/etc/hosts", "predefined": {"a.lab": "10.0.0.1"}}
		],
		"rules": [
			{"domain": ["ha.home.lab", "truenas.home.lab"], "server": "static-hosts"},
			{"domain": ["a.lab"], "server": "file-hosts"}
		]
	}
}`

// testRecords — записи для синхронизации.
var testRecords = []DNSRecord{
	{
		Domain:    "ha.home.lab",
		Addresses: []string{"192.168.50.8"},
	},
	{
		Domain:    "truenas.home.lab",
		Addresses: []string{"192.168.50.19"},
	},
}

func TestImportHostsServers(t *testing.T) {
	config := parseConfig(t, staticHostsConfig)

	records, err := ImportHostsServers(config)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(records, testRecords) {
		t.Errorf("records = %v, want %v", records, testRecords)
	}

	// Сервер с файлом hosts не переносится.
	want := []string{
		`{"domain":["a.lab"],"server":"file-hosts"}`,
	}

	if got := dnsRules(t, config); !slices.Equal(got, want) {
		t.Errorf("dns rules = %v, want %v", got, want)
	}

	servers := config["dns"].(map[string]any)["servers"].([]any)

	if len(servers) != 2 {
		t.Errorf("servers = %v, want yandex and file-hosts", servers)
	}
}

func TestImportHostsServersSkipsComplexRules(t *testing.T) {
	config := parseConfig(t, `{
		"dns": {
			"servers": [{"type": "hosts", "tag": "static-hosts", "predefined": {"ha.home.lab": "192.168.50.8"}}],
			"rules": [{"domain": ["ha.home.lab"], "query_type": ["A"], "server": "static-hosts"}]
		}
	}`)

	records, err := ImportHostsServers(config)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 0 || len(dnsRules(t, config)) != 1 {
		t.Errorf("server with complex rule must stay: records=%v", records)
	}
}

func TestSyncDNSRecords(t *testing.T) {
	config := parseConfig(t, manualDNSConfig)

	if err := syncGroups(config, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	if err := SyncDNSRecords(config, testRecords); err != nil {
		t.Fatal(err)
	}

	rules := dnsRules(t, config)

	if rules[0] != `{"domain":["ha.home.lab","truenas.home.lab"],"server":"configurer-hosts"}` {
		t.Errorf("first dns rule = %s", rules[0])
	}

	if !CheckDNSRecordsSync(config, testRecords) {
		t.Error("records should be synced")
	}

	// Повторная синхронизация групп оставляет правило записей первым.
	config = roundTrip(t, config)

	if err := syncGroups(config, testGroups, testGroups); err != nil {
		t.Fatal(err)
	}

	if !CheckDNSRecordsSync(config, testRecords) {
		t.Errorf("records should stay synced after groups sync: %v", dnsRules(t, config))
	}

	if CheckDNSRecordsSync(config, testRecords[:1]) {
		t.Error("changed records should not be synced")
	}

	if err := SyncDNSRecords(config, nil); err != nil {
		t.Fatal(err)
	}

	if !CheckDNSRecordsSync(config, nil) {
		t.Error("empty records should remove hosts server and rule")
	}

	for _, server := range config["dns"].(map[string]any)["servers"].([]any) {
		if isHostsServer(server) {
			t.Error("hosts server should be removed")
		}
	}
}
