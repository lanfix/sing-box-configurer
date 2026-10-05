package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// TestDefaultsForOldData проверяет, что у данных прежних версий без разделов happ и security включаются
// применение подписок и проверка адреса панели, а сохраненные значения не перезаписываются.
func TestDefaultsForOldData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")

	if err := os.WriteFile(path, []byte(`{"settings": {"log_level": "info", "clash_api": {"secret": "s", "allow_origins": []}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(appdata.NewFile(path))
	if err != nil {
		t.Fatal(err)
	}

	data := manager.Get()

	if !data.Happ.AutoApply || !data.Security.CheckHost || data.Security.AllowedHosts == nil || data.LogLevel != "info" {
		t.Errorf("settings = %+v", data)
	}

	if err = manager.UpdateHapp(Happ{AutoApply: false}); err != nil {
		t.Fatal(err)
	}

	if err = manager.UpdateSecurity(Security{CheckHost: false, AllowedHosts: []string{"Router.lan"}}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManager(appdata.NewFile(path))
	if err != nil {
		t.Fatal(err)
	}

	data = reloaded.Get()

	if data.Happ.AutoApply || data.Security.CheckHost || len(data.Security.AllowedHosts) != 1 || data.Security.AllowedHosts[0] != "router.lan" {
		t.Errorf("saved settings must be kept: %+v", data)
	}
}

// TestUpdatesSettings проверяет, что у данных прежних версий автоматическая проверка обновлений выключена,
// а недопустимый интервал не сохраняется.
func TestUpdatesSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")

	if err := os.WriteFile(path, []byte(`{"settings": {"log_level": "info", "clash_api": {"secret": "s", "allow_origins": []}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(appdata.NewFile(path))
	if err != nil {
		t.Fatal(err)
	}

	if got := manager.Get().Updates; got != DefaultUpdates() {
		t.Errorf("updates = %+v, want defaults", got)
	}

	if err = manager.SetUpdates(Updates{AutoCheck: true, IntervalHours: 5}); err == nil {
		t.Error("interval 5 h must be rejected")
	}

	if err = manager.SetUpdates(Updates{AutoCheck: true, IntervalHours: 12}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManager(appdata.NewFile(path))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := reloaded.Get().Updates, (Updates{AutoCheck: true, IntervalHours: 12}); got != want {
		t.Errorf("saved updates = %+v, want %+v", got, want)
	}
}
