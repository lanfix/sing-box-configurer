package migrations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// fakeSingBox хранит конфиг sing-box в памяти.
type fakeSingBox struct {
	config map[string]any
	writes int
}

func (f *fakeSingBox) GetActualConfigParsed() (map[string]any, error) {
	return f.config, nil
}

func (f *fakeSingBox) WriteActualConfig(config map[string]any) error {
	f.config = config
	f.writes++

	return nil
}

// newAppData создает app.json с содержимым content (пустая строка — файла нет).
func newAppData(t *testing.T, content string) (*appdata.File, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.json")

	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return appdata.NewFile(path), path
}

// readFields читает поля app.json.
func readFields(t *testing.T, path string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]any

	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}

	return fields
}

// testList — миграции для тестов: переименовывают поле и добавляют outbound в конфиг sing-box.
var testList = []Migration{
	{
		Version: 1,
		Name:    "rename old_field",
		Up: func(state *State) error {
			if raw, ok := state.AppData["old_field"]; ok {
				state.AppData["new_field"] = raw
				delete(state.AppData, "old_field")
			}

			return nil
		},
	},
	{
		Version: 2,
		Name:    "add outbound",
		Up: func(state *State) error {
			config, err := state.SingBoxConfig()
			if err != nil {
				return err
			}

			config["outbounds"] = []any{
				map[string]any{
					"type": "direct",
					"tag":  "direct",
				},
			}
			state.MarkSingBoxConfigChanged()

			return nil
		},
	},
}

// TestRunFreshInstall проверяет, что новая инсталляция сразу получает последнюю версию схемы.
func TestRunFreshInstall(t *testing.T) {
	appData, path := newAppData(t, "")
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	result, err := run(appData, singBox, testList)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Applied) != 0 || singBox.writes != 0 {
		t.Errorf("fresh install must not run migrations: %+v, writes=%d", result, singBox.writes)
	}

	if readFields(t, path)["schema_version"] != float64(2) {
		t.Errorf("schema_version must be 2")
	}
}

// TestRunLegacyData проверяет применение всех миграций к данным без schema_version.
func TestRunLegacyData(t *testing.T) {
	appData, path := newAppData(t, `{"rules": [], "old_field": "value"}`)
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	result, err := run(appData, singBox, testList)
	if err != nil {
		t.Fatal(err)
	}

	if result.FromVersion != 0 || result.ToVersion != 2 || len(result.Applied) != 2 {
		t.Errorf("unexpected result: %+v", result)
	}

	fields := readFields(t, path)

	if _, ok := fields["old_field"]; ok {
		t.Error("old_field must be removed")
	}

	if fields["new_field"] != "value" || fields["schema_version"] != float64(2) {
		t.Errorf("unexpected fields: %v", fields)
	}

	if singBox.writes != 1 || singBox.config["outbounds"] == nil {
		t.Errorf("sing-box config must be written once: writes=%d", singBox.writes)
	}

	// Повторный запуск ничего не делает.
	result, err = run(appData, singBox, testList)
	if err != nil || len(result.Applied) != 0 || singBox.writes != 1 {
		t.Errorf("second run must be no-op: %+v, %v, writes=%d", result, err, singBox.writes)
	}
}

// TestRunNewerSchema проверяет отказ работать с данными более новой версии.
func TestRunNewerSchema(t *testing.T) {
	appData, _ := newAppData(t, `{"schema_version": 5}`)
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	if _, err := run(appData, singBox, testList); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("expected ErrNewerSchema, got %v", err)
	}
}

// TestRunFailureKeepsFiles проверяет, что при ошибке миграции файлы не меняются.
func TestRunFailureKeepsFiles(t *testing.T) {
	original := `{"old_field": "value"}`
	appData, path := newAppData(t, original)
	singBox := &fakeSingBox{
		config: map[string]any{},
		writes: 0,
	}

	failing := append([]Migration{}, testList...)
	failing = append(failing, Migration{
		Version: 3,
		Name:    "broken",
		Up: func(_ *State) error {
			return errors.New("boom")
		},
	})

	if _, err := run(appData, singBox, failing); err == nil {
		t.Fatal("expected error")
	}

	raw, _ := os.ReadFile(path)

	if string(raw) != original || singBox.writes != 0 {
		t.Errorf("files must stay untouched: app.json=%s writes=%d", raw, singBox.writes)
	}
}

// TestValidate проверяет контроль нумерации миграций.
func TestValidate(t *testing.T) {
	if err := validate(registry); err != nil {
		t.Fatalf("registry is invalid: %v", err)
	}

	broken := []Migration{
		{
			Version: 2,
			Name:    "gap",
			Up: func(_ *State) error {
				return nil
			},
		},
	}

	if err := validate(broken); err == nil {
		t.Error("expected error for version gap")
	}
}

// TestMigrateGroupDNSServers проверяет перенос DNS-серверов групп из ручных DNS-правил.
func TestMigrateGroupDNSServers(t *testing.T) {
	appData, path := newAppData(t, `{
		"schema_version": 2,
		"groups": [
			{"name": "default", "description": "Группа по умолчанию"},
			{"name": "claude", "dns_server": "google"},
			{"name": "block"}
		]
	}`)

	singBox := &fakeSingBox{
		config: map[string]any{
			"dns": map[string]any{
				"rules": []any{
					map[string]any{
						"rule_set": "configurer-default",
						"server":   "cloudflare",
					},
					map[string]any{
						"rule_set": "configurer-claude",
						"server":   "cloudflare",
					},
				},
			},
		},
		writes: 0,
	}

	if _, err := run(appData, singBox, registry); err != nil {
		t.Fatal(err)
	}

	groups, _ := readFields(t, path)["groups"].([]any)
	servers := map[string]any{}

	for _, item := range groups {
		group := item.(map[string]any)
		servers[group["name"].(string)] = group["dns_server"]
	}

	// Уже заданный DNS-сервер не перезаписывается, у группы без правила он не появляется.
	want := map[string]any{
		"default": "cloudflare",
		"claude":  "google",
		"block":   nil,
	}

	for name, server := range want {
		if servers[name] != server {
			t.Errorf("group %s dns_server = %v, want %v", name, servers[name], server)
		}
	}

	if singBox.writes != 0 {
		t.Errorf("sing-box config must not be written, writes=%d", singBox.writes)
	}
}

// TestMigrateHostsToDNSRecords проверяет перенос hosts-сервера из конфига sing-box в DNS-записи.
func TestMigrateHostsToDNSRecords(t *testing.T) {
	appData, path := newAppData(t, `{"schema_version": 3}`)

	singBox := &fakeSingBox{
		config: map[string]any{
			"dns": map[string]any{
				"servers": []any{
					map[string]any{
						"type":       "hosts",
						"tag":        "static-hosts",
						"predefined": map[string]any{"ha.home.lab": []any{"192.168.50.8"}},
					},
				},
				"rules": []any{
					map[string]any{
						"action":     "predefined",
						"query_type": []any{"HTTPS"},
						"rcode":      "NOERROR",
					},
					map[string]any{
						"domain": []any{"ha.home.lab"},
						"server": "static-hosts",
					},
				},
			},
		},
		writes: 0,
	}

	if _, err := run(appData, singBox, registry); err != nil {
		t.Fatal(err)
	}

	records, _ := readFields(t, path)["dns_records"].([]any)

	if len(records) != 1 || records[0].(map[string]any)["domain"] != "ha.home.lab" {
		t.Fatalf("dns_records = %v", records)
	}

	if singBox.writes != 1 {
		t.Fatalf("sing-box config must be written once, writes=%d", singBox.writes)
	}

	dns := singBox.config["dns"].(map[string]any)
	servers := dns["servers"].([]any)
	rules := dns["rules"].([]any)

	if len(servers) != 1 || servers[0].(map[string]any)["tag"] != "configurer-hosts" {
		t.Errorf("servers = %v", servers)
	}

	// Правило записей становится первым, HTTPS-фильтр остается.
	if rules[0].(map[string]any)["server"] != "configurer-hosts" || len(rules) != 2 {
		t.Errorf("rules = %v", rules)
	}
}

// TestMigrateConfigToAppData проверяет перенос настроек конфига sing-box в app.json (миграция 5).
func TestMigrateConfigToAppData(t *testing.T) {
	appData, path := newAppData(t, `{
		"schema_version": 4,
		"rules": [
			{"id": "1", "value": "a.ru", "group": "default", "bypass": true},
			{"id": "2", "value": "b.com", "group": "default", "bypass": false}
		],
		"url_sources": [{"id": "s", "group": "claude", "bypass": true}],
		"groups": [{"name": "default"}, {"name": "block"}],
		"happ": {"profiles": [{"synced_tags": ["sub-1"]}]}
	}`)

	var config map[string]any

	err := json.Unmarshal([]byte(`{
		"log": {"level": "info"},
		"dns": {
			"servers": [
				{"type": "udp", "tag": "yandex", "server": "77.88.8.1"},
				{"type": "hosts", "tag": "configurer-hosts", "predefined": {"a.lab": ["10.0.0.1"]}}
			],
			"rules": [
				{"domain": ["a.lab"], "server": "configurer-hosts"},
				{"action": "predefined", "query_type": ["HTTPS"], "rcode": "NOERROR"},
				{"rule_set": "configurer-default", "server": "yandex"},
				{"domain": ["claude.ai"], "server": "yandex"}
			],
			"final": "yandex",
			"independent_cache": true
		},
		"inbounds": [
			{"type": "tun", "tag": "tun-in"},
			{"type": "mixed", "tag": "mixed-proxy", "listen": "0.0.0.0", "listen_port": 1080, "users": [{"username": "u", "password": "p"}]}
		],
		"outbounds": [
			{"type": "vless", "tag": "manual"},
			{"type": "vless", "tag": "sub-1"},
			{"type": "urltest", "tag": "auto", "outbounds": ["manual"]},
			{"type": "direct", "tag": "direct"},
			{"type": "selector", "tag": "select-default", "outbounds": ["manual"]}
		],
		"endpoints": [{"type": "wireguard", "tag": "wg"}],
		"route": {"default_domain_resolver": {"server": "yandex"}},
		"experimental": {"clash_api": {"secret": "old-secret", "access_control_allow_origin": ["http://yacd"]}}
	}`), &config)
	if err != nil {
		t.Fatal(err)
	}

	singBox := &fakeSingBox{
		config: config,
		writes: 0,
	}

	if _, err = run(appData, singBox, registry); err != nil {
		t.Fatal(err)
	}

	if singBox.writes != 0 {
		t.Error("sing-box config must not be changed")
	}

	fields := readFields(t, path)

	check := func(key, want string) {
		t.Helper()

		raw, _ := json.Marshal(fields[key])

		if string(raw) != want {
			t.Errorf("%s:\n got %s\nwant %s", key, raw, want)
		}
	}

	check("rules", `[{"group":"bypass","id":"1","value":"a.ru"},{"group":"default","id":"2","value":"b.com"}]`)
	check("url_sources", `[{"group":"bypass","id":"s"}]`)
	check("groups", `[{"name":"default"}]`)
	check("inbounds", `{"mixed":[{"listen":"0.0.0.0","listen_port":1080,"tag":"mixed-proxy","users":[{"password":"p","username":"u"}]}]}`)
	check("settings", `{"clash_api":{"allow_origins":["http://yacd"],"secret":"old-secret"},"log_level":"info"}`)

	dns := fields["dns"].(map[string]any)

	raw, _ := json.Marshal(dns["servers"])
	if string(raw) != `[{"server":"77.88.8.1","tag":"yandex","type":"udp"}]` {
		t.Errorf("dns servers = %s", raw)
	}

	raw, _ = json.Marshal(dns["rules"])
	if string(raw) != `[{"domain":["claude.ai"],"server":"yandex"}]` {
		t.Errorf("dns rules = %s", raw)
	}

	raw, _ = json.Marshal(dns["settings"])
	if string(raw) != `{"default_domain_resolver":"yandex","extra":{"independent_cache":true},"final":"yandex"}` {
		t.Errorf("dns settings = %s", raw)
	}

	outbounds := fields["outbounds"].([]any)
	tags := make([]string, 0, len(outbounds))

	for _, item := range outbounds {
		tags = append(tags, item.(map[string]any)["config"].(map[string]any)["tag"].(string))
	}

	if raw, _ = json.Marshal(tags); string(raw) != `["manual","wg"]` {
		t.Errorf("outbounds = %s", raw)
	}
}

// TestMigrateFixShareOutbounds проверяет исправление транспорта и диапазонов портов outbound-ов (миграция 6).
func TestMigrateFixShareOutbounds(t *testing.T) {
	appData, path := newAppData(t, `{
		"schema_version": 5,
		"outbounds": [
			{"id": "1", "config": {"type": "trojan", "tag": "ws", "transport": {"type": "ws", "ws": {"path": "/cdn", "headers": {"Host": "h"}}}}},
			{"id": "2", "config": {"type": "vless", "tag": "x", "transport": {"type": "httpupgrade", "xhttp": {"path": "/x", "host": "h", "mode": "auto"}}}},
			{"id": "3", "config": {"type": "hysteria2", "tag": "hop", "server_ports": ["20000-29999", "443"]}}
		]
	}`)

	if _, err := run(appData, &fakeSingBox{}, registry); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(readFields(t, path)["outbounds"])
	want := `[{"config":{"tag":"ws","transport":{"headers":{"Host":"h"},"path":"/cdn","type":"ws"},"type":"trojan"},"id":"1"},` +
		`{"config":{"tag":"x","transport":{"host":"h","path":"/x","type":"httpupgrade"},"type":"vless"},"id":"2"},` +
		`{"config":{"server_ports":["20000:29999","443"],"tag":"hop","type":"hysteria2"},"id":"3"}]`

	if string(raw) != want {
		t.Errorf("outbounds:\n got %s\nwant %s", raw, want)
	}
}
