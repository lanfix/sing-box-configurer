package inbounds

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

func TestMixedOutbound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")

	manager, err := NewManager(appdata.NewFile(path))
	if err != nil {
		t.Fatal(err)
	}

	for _, mixed := range []Mixed{
		{Tag: "mixed-proxy", Listen: "", ListenPort: 1080, Users: nil, Outbound: "", Extra: nil},
		{Tag: "mixed-vpn", Listen: "", ListenPort: 1081, Users: nil, Outbound: " select-default ", Extra: nil},
	} {
		if err = manager.AddMixed(mixed); err != nil {
			t.Fatal(err)
		}
	}

	reloaded, err := NewManager(appdata.NewFile(path))
	if err != nil {
		t.Fatal(err)
	}

	if got := reloaded.UsingOutbound("select-default"); !slices.Equal(got, []string{"mixed-vpn"}) {
		t.Errorf("UsingOutbound = %v", got)
	}

	// Пустой тег — не ссылка на outbound.
	if got := reloaded.UsingOutbound(""); len(got) != 0 {
		t.Errorf("UsingOutbound(\"\") = %v", got)
	}

	// Поле outbound — настройка конфигуратора, в inbound sing-box оно не попадает.
	if config := reloaded.Mixed()[1].Config(); config["outbound"] != nil {
		t.Errorf("config = %v", config)
	}
}
