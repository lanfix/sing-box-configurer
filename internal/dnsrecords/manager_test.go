package dnsrecords

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// newTestManager создает менеджер с пустым app.json во временной директории.
func newTestManager(t *testing.T) (*Manager, *appdata.File) {
	t.Helper()

	appData := appdata.NewFile(filepath.Join(t.TempDir(), "app.json"))

	manager, err := NewManager(appData)
	if err != nil {
		t.Fatal(err)
	}

	return manager, appData
}

func TestAddNormalizesAndPersists(t *testing.T) {
	manager, appData := newTestManager(t)

	record, err := manager.Add(" HA.Home.Lab. ", []string{"192.168.50.8", " ", "192.168.50.8", "::1"}, "Home Assistant")
	if err != nil {
		t.Fatal(err)
	}

	if record.Domain != "ha.home.lab" || !slices.Equal(record.Addresses, []string{"192.168.50.8", "::1"}) {
		t.Errorf("record = %+v", record)
	}

	reloaded, err := NewManager(appData)
	if err != nil {
		t.Fatal(err)
	}

	if list := reloaded.List(); len(list) != 1 || list[0].ID != record.ID {
		t.Errorf("reloaded records = %+v", list)
	}
}

func TestAddValidates(t *testing.T) {
	manager, _ := newTestManager(t)

	cases := []struct {
		domain    string
		addresses []string
	}{
		{domain: "", addresses: []string{"10.0.0.1"}},
		{domain: "bad domain", addresses: []string{"10.0.0.1"}},
		{domain: "*.lab", addresses: []string{"10.0.0.1"}},
		{domain: "ok.lab", addresses: []string{}},
		{domain: "ok.lab", addresses: []string{"10.0.0.300"}},
	}

	for _, c := range cases {
		if _, err := manager.Add(c.domain, c.addresses, ""); err == nil {
			t.Errorf("Add(%q, %v): expected error", c.domain, c.addresses)
		}
	}

	if _, err := manager.Add("ok.lab", []string{"10.0.0.1"}, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Add("OK.lab", []string{"10.0.0.2"}, ""); err == nil {
		t.Error("duplicate domain: expected error")
	}
}

func TestEditAndDelete(t *testing.T) {
	manager, _ := newTestManager(t)

	first, _ := manager.Add("a.lab", []string{"10.0.0.1"}, "")
	second, _ := manager.Add("b.lab", []string{"10.0.0.2"}, "")

	if err := manager.Edit(second.ID, "a.lab", []string{"10.0.0.3"}, ""); err == nil {
		t.Error("edit to existing domain: expected error")
	}

	if err := manager.Edit(second.ID, "c.lab", []string{"10.0.0.3"}, "C"); err != nil {
		t.Fatal(err)
	}

	if err := manager.Delete(first.ID); err != nil {
		t.Fatal(err)
	}

	records := manager.List()

	if len(records) != 1 || records[0].Domain != "c.lab" || records[0].Addresses[0] != "10.0.0.3" {
		t.Errorf("records = %+v", records)
	}

	if err := manager.Delete(first.ID); err == nil {
		t.Error("delete missing record: expected error")
	}
}
