package devices

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/platform"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// fakeNetwork отдает заданный вывод команд ip.
type fakeNetwork struct {
	output platform.NetworkOutput
	err    error
}

// Read возвращает заданный вывод.
func (n *fakeNetwork) Read(_ context.Context) (platform.NetworkOutput, error) {
	return n.output, n.err
}

// fakeSingBox принимает или отклоняет конфиг в Check.
type fakeSingBox struct {
	exitCode int
	err      error
	checks   int
}

func (s *fakeSingBox) Check(_ context.Context, _ []byte) (platform.CheckResult, error) {
	s.checks++

	return platform.CheckResult{ExitCode: s.exitCode, Output: ""}, s.err
}

func (s *fakeSingBox) Restart(_ context.Context) error { return nil }

func (s *fakeSingBox) State(_ context.Context) (platform.State, error) { return platform.State{}, nil }

func (s *fakeSingBox) Logs(_ context.Context, _ int) (string, error) { return "", nil }

// fakeVersions возвращает заданную версию sing-box.
type fakeVersions struct {
	version string
}

func (v *fakeVersions) GetVersion() (string, error) {
	return v.version, nil
}

// supportedSingBox возвращает sing-box, который поддерживает MAC-адреса в rule-set-ах.
func supportedSingBox() *fakeSingBox {
	return &fakeSingBox{exitCode: 0, err: nil, checks: 0}
}

// newTestManager создает менеджер с данными во временном app.json.
func newTestManager(t *testing.T, opts Options) *Manager {
	t.Helper()

	manager, err := NewManager(appdata.NewFile(filepath.Join(t.TempDir(), "app.json")), opts)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

// toJSON возвращает JSON значения для сравнения.
func toJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestRuleSets(t *testing.T) {
	network := &fakeNetwork{
		output: platform.NetworkOutput{Addresses: iproute2Addresses, Neighbors: busyboxNeighbors},
		err:    nil,
	}
	manager := newTestManager(t, Options{Network: network, SingBox: supportedSingBox(), Versions: nil})

	if err := manager.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, device := range []Device{
		{MAC: "AA-BB-CC-DD-EE-01", Name: "Телевизор", Profile: ProfileProxy},
		{MAC: "aa:bb:cc:dd:ee:02", Name: "Гостевой", Profile: ProfileBlocked},
		{MAC: "aa:bb:cc:dd:ee:03", Name: "Принтер", Profile: ProfileDefault},
	} {
		if err := manager.Add(device); err != nil {
			t.Fatal(err)
		}
	}

	settings := DefaultSettings()
	settings.DefaultProfile = ProfileDirect
	settings.Exclude = []string{"10.8.0.0/24", "192.168.50.200"}

	if err := manager.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	direct, err := manager.RuleSet(context.Background(), ProfileDirect)
	if err != nil {
		t.Fatal(err)
	}

	// Неизвестные: из сетей LAN, не хост и не исключения, MAC не из устройств со своим профилем.
	want := `{"version":4,"rules":[{"mode":"and","rules":[` +
		`{"source_ip_cidr":["192.168.50.0/24","fd00:50::/64"]},` +
		`{"invert":true,"source_ip_cidr":["172.17.0.1/32","172.18.0.1/32","192.168.50.6/32","198.18.0.1/32","fd00:50::6/128","10.8.0.0/24","192.168.50.200/32"]},` +
		`{"invert":true,"source_mac_address":["aa:bb:cc:dd:ee:01","aa:bb:cc:dd:ee:02"]}` +
		`],"type":"logical"}]}`

	if got := toJSON(t, direct); got != want {
		t.Errorf("direct rule-set:\n got %s\nwant %s", got, want)
	}

	blocked, err := manager.RuleSet(context.Background(), ProfileBlocked)
	if err != nil {
		t.Fatal(err)
	}

	if got := toJSON(t, blocked); got != `{"version":4,"rules":[{"source_mac_address":["aa:bb:cc:dd:ee:02"]}]}` {
		t.Errorf("blocked rule-set = %s", got)
	}

	if _, err = manager.RuleSet(context.Background(), ProfileProxy); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("proxy rule-set error = %v", err)
	}

	state := manager.State()

	// Оба найденных устройства добавлены, неизвестных нет.
	if len(state.Unknown) != 0 || manager.UnknownCount() != 0 {
		t.Errorf("unknown = %+v", state.Unknown)
	}

	if mac, name, ok := manager.LookupIP("192.168.50.10"); !ok || mac != "aa:bb:cc:dd:ee:01" || name != "Телевизор" {
		t.Errorf("LookupIP = %s %s %v", mac, name, ok)
	}
}

func TestRuleSetWithoutNetworks(t *testing.T) {
	manager := newTestManager(t, Options{Network: nil, SingBox: supportedSingBox(), Versions: nil})

	settings := DefaultSettings()
	settings.DefaultProfile = ProfileBlocked

	if err := manager.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Без сетей LAN правила для неизвестных нет: иначе под него попал бы любой трафик.
	blocked, err := manager.RuleSet(context.Background(), ProfileBlocked)
	if err != nil {
		t.Fatal(err)
	}

	if got := toJSON(t, blocked); got != `{"version":4,"rules":[]}` {
		t.Errorf("blocked rule-set = %s", got)
	}

	settings.AutoNetworks = false
	settings.Networks = []string{"192.168.1.0/24"}

	if err = manager.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	blocked, _ = manager.RuleSet(context.Background(), ProfileBlocked)

	if got := toJSON(t, blocked); got != `{"version":4,"rules":[{"source_ip_cidr":["192.168.1.0/24"]}]}` {
		t.Errorf("blocked rule-set = %s", got)
	}
}

func TestScanKeepsDetectedOnEmptyTable(t *testing.T) {
	network := &fakeNetwork{
		output: platform.NetworkOutput{Addresses: iproute2Addresses, Neighbors: busyboxNeighbors},
		err:    nil,
	}
	manager := newTestManager(t, Options{Network: network, SingBox: nil, Versions: nil})

	if err := manager.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	network.output.Neighbors = ""

	if err := manager.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	network.err = errors.New("sing-box is not running")

	if err := manager.Scan(context.Background()); err == nil {
		t.Fatal("scan error is lost")
	}

	state := manager.State()

	if toJSON(t, state.Networks) != `["192.168.50.0/24","fd00:50::/64"]` {
		t.Errorf("networks = %v", state.Networks)
	}

	if state.ScanError == "" || len(state.Unknown) != 2 || state.Unknown[0].Online {
		t.Errorf("state = %+v", state)
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")

	manager, err := NewManager(appdata.NewFile(path), Options{Network: nil, SingBox: nil, Versions: nil})
	if err != nil {
		t.Fatal(err)
	}

	if err = manager.Add(Device{MAC: "aa:bb:cc:dd:ee:01", Name: "ТВ", Profile: ProfileDirect}); err != nil {
		t.Fatal(err)
	}

	if err = manager.Add(Device{MAC: "AA:BB:CC:DD:EE:01", Name: "", Profile: ProfileDirect}); err == nil {
		t.Error("duplicate MAC accepted")
	}

	if err = manager.Edit(Device{MAC: "aa:bb:cc:dd:ee:01", Name: "ТВ", Profile: ProfileBlocked}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManager(appdata.NewFile(path), Options{Network: nil, SingBox: nil, Versions: nil})
	if err != nil {
		t.Fatal(err)
	}

	state := reloaded.State()

	if len(state.Devices) != 1 || state.Devices[0].Profile != ProfileBlocked || state.Settings.DefaultProfile != ProfileProxy {
		t.Errorf("reloaded state = %+v", state)
	}

	if err = reloaded.Delete("AA-BB-CC-DD-EE-01"); err != nil {
		t.Fatal(err)
	}

	if err = reloaded.Delete("aa:bb:cc:dd:ee:01"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v", err)
	}
}

func TestValidation(t *testing.T) {
	manager := newTestManager(t, Options{Network: nil, SingBox: nil, Versions: nil})

	for _, device := range []Device{
		{MAC: "not-a-mac", Name: "", Profile: ProfileDirect},
		{MAC: "aa:bb:cc:dd:ee:01", Name: "", Profile: "vpn"},
	} {
		if err := manager.Add(device); err == nil {
			t.Errorf("device %+v accepted", device)
		}
	}

	for _, settings := range []Settings{
		{DefaultProfile: ProfileDefault, AutoNetworks: true, Networks: nil, Exclude: nil, DirectDNSServer: ""},
		{DefaultProfile: ProfileDirect, AutoNetworks: true, Networks: []string{"192.168.1.0/33"}, Exclude: nil, DirectDNSServer: ""},
		{DefaultProfile: ProfileDirect, AutoNetworks: true, Networks: nil, Exclude: []string{"host"}, DirectDNSServer: ""},
	} {
		if err := manager.UpdateSettings(settings); err == nil {
			t.Errorf("settings %+v accepted", settings)
		}
	}

	settings := Settings{
		DefaultProfile:  ProfileDirect,
		AutoNetworks:    false,
		Networks:        []string{" 192.168.1.77/24 ", "192.168.1.0/24", ""},
		Exclude:         []string{"::ffff:10.0.0.1"},
		DirectDNSServer: " local ",
	}

	if err := manager.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	saved := manager.Settings()

	if toJSON(t, saved.Networks) != `["192.168.1.0/24"]` || toJSON(t, saved.Exclude) != `["10.0.0.1/32"]` || saved.DirectDNSServer != "local" {
		t.Errorf("normalized settings = %+v", saved)
	}
}

func TestSupport(t *testing.T) {
	ctx := context.Background()
	singBox := &fakeSingBox{exitCode: 1, err: nil, checks: 0}
	versions := &fakeVersions{version: "sing-box 1.14.1-lx.8"}

	manager := newTestManager(t, Options{Network: nil, SingBox: singBox, Versions: versions})

	if err := manager.Add(Device{MAC: "aa:bb:cc:dd:ee:01", Name: "", Profile: ProfileBlocked}); err != nil {
		t.Fatal(err)
	}

	// Без результата проверки sing-box проверяется сразу, старый sing-box получает пустой rule-set.
	blocked, _ := manager.RuleSet(ctx, ProfileBlocked)

	if manager.Enabled() || len(blocked.Rules) != 0 || singBox.checks != 1 {
		t.Errorf("old sing-box: checks=%d rule-set=%+v", singBox.checks, blocked)
	}

	// Та же версия повторно не проверяется, новая — проверяется.
	manager.RefreshSupport(ctx)
	manager.RefreshSupport(ctx)

	singBox.exitCode = 0
	versions.version = "sing-box 1.14.2-lx.11-mac.1"
	manager.RefreshSupport(ctx)

	if blocked, _ = manager.RuleSet(ctx, ProfileBlocked); singBox.checks != 3 || !manager.Enabled() || len(blocked.Rules) != 1 {
		t.Errorf("new sing-box: checks=%d rule-set=%+v", singBox.checks, blocked)
	}

	// Clash API недоступен, проверка не удалась (sing-box не запущен): остается прежний результат.
	singBox.err = errors.New("not running")
	versions.version = ""
	manager.RefreshSupport(ctx)

	// Рендер сразу после неудачной проверки ее не повторяет.
	if support := manager.State().Support; !manager.Enabled() || support.Error == "" || singBox.checks != 4 {
		t.Errorf("support after error = %+v, checks=%d", support, singBox.checks)
	}

	// Бинарник заменили старым: запускающийся sing-box запрашивает rule-set, и проверка повторяется сразу.
	singBox.err = nil
	singBox.exitCode = 1

	if blocked, _ = manager.RuleSet(ctx, ProfileBlocked); len(blocked.Rules) != 0 || singBox.checks != 5 {
		t.Errorf("replaced sing-box: checks=%d rule-set=%+v", singBox.checks, blocked)
	}

	// Без sing-box профили не рендерятся.
	if newTestManager(t, Options{Network: nil, SingBox: nil, Versions: nil}).Enabled() {
		t.Error("enabled without sing-box")
	}
}
