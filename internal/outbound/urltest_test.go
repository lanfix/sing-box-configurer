package outbound

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// testCandidates — outbound-ы всех источников.
func testCandidates() []Candidate {
	return []Candidate{
		{Tag: "manual-de", Type: "vless", Source: SourceManual},
		{Tag: "manual-select", Type: "selector", Source: SourceManual},
		{Tag: "happ-a-de", Type: "vless", Source: SourceHapp, ProfileID: "a"},
		{Tag: "happ-a-nl", Type: "trojan", Source: SourceHapp, ProfileID: "a"},
		{Tag: "happ-a-ru", Type: "vless", Source: SourceHapp, ProfileID: "a"},
		{Tag: "happ-b-de", Type: "vless", Source: SourceHapp, ProfileID: "b"},
		{Tag: "amnezia-de", Type: "wireguard", Source: SourceAmnezia, ProfileID: "c"},
	}
}

func TestURLTestResolve(t *testing.T) {
	tests := []struct {
		name    string
		urlTest URLTest
		want    []Member
	}{
		{
			name:    "all sources skip groups",
			urlTest: URLTest{Sources: []URLTestSource{{Kind: SourceAll}}, IncludeRegexp: "-de$"},
			want: []Member{
				{Tag: "manual-de", State: MemberIncluded},
				{Tag: "happ-a-de", State: MemberIncluded},
				{Tag: "happ-b-de", State: MemberIncluded},
				{Tag: "amnezia-de", State: MemberIncluded},
			},
		},
		{
			name: "whole profile with exclusions",
			urlTest: URLTest{
				Sources:       []URLTestSource{{Kind: SourceHapp, ProfileID: "a"}},
				ExcludeRegexp: "(?i)RU",
				ExcludeTags:   []string{"happ-a-nl"},
			},
			want: []Member{
				{Tag: "happ-a-de", State: MemberIncluded},
				{Tag: "happ-a-nl", State: MemberExcludedTag},
				{Tag: "happ-a-ru", State: MemberExcludedRegexp},
			},
		},
		{
			name: "all profiles of source and explicit tags",
			urlTest: URLTest{
				Sources: []URLTestSource{{Kind: SourceHapp}},
				// Явные теги: группа, уже подобранный outbound и отсутствующий outbound.
				Tags:          []string{"manual-select", "happ-b-de", "gone"},
				IncludeRegexp: "b-",
			},
			want: []Member{
				{Tag: "happ-b-de", State: MemberIncluded},
				{Tag: "manual-select", State: MemberIncluded},
				{Tag: "gone", State: MemberMissing},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.urlTest.Resolve(testCandidates())
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("members:\n got %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestURLTestValidate(t *testing.T) {
	valid := URLTest{Tag: "fast", Sources: []URLTestSource{{Kind: SourceAll}}, Interval: "30s", URL: DefaultTestURL}

	if err := valid.Validate(); err != nil {
		t.Errorf("valid urltest: %v", err)
	}

	invalid := map[string]URLTest{
		"empty tag":      {Sources: valid.Sources},
		"reserved tag":   {Tag: "select-x", Sources: valid.Sources},
		"no members":     {Tag: "fast"},
		"bad source":     {Tag: "fast", Sources: []URLTestSource{{Kind: "other"}}},
		"bad regexp":     {Tag: "fast", Sources: valid.Sources, IncludeRegexp: "("},
		"bad interval":   {Tag: "fast", Sources: valid.Sources, Interval: "3 minutes"},
		"bad url":        {Tag: "fast", Sources: valid.Sources, URL: "gstatic.com"},
		"self reference": {Tag: "fast", Tags: []string{"fast"}},
	}

	for name, urlTest := range invalid {
		if err := urlTest.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestManagerURLTests(t *testing.T) {
	appData := appdata.NewFile(filepath.Join(t.TempDir(), "app.json"))

	manager, err := NewManager(appData)
	if err != nil {
		t.Fatal(err)
	}

	// Новая инсталляция получает urltest auto.
	urlTests := manager.ListURLTests()
	if len(urlTests) != 1 || urlTests[0].Tag != AutoTag {
		t.Fatalf("urltests = %v", urlTests)
	}

	// Тег urltest-а занят для outbound-ов, и наоборот.
	if _, err = manager.Add(map[string]any{"type": "vless", "tag": AutoTag}); err == nil {
		t.Error("outbound tag must not collide with urltest")
	}

	if _, err = manager.Add(map[string]any{"type": "vless", "tag": "de"}); err != nil {
		t.Fatal(err)
	}

	if _, err = manager.AddURLTest(URLTest{Tag: "de", Sources: []URLTestSource{{Kind: SourceAll}}}); err == nil {
		t.Error("urltest tag must not collide with outbound")
	}

	// auto можно удалить, и после перезапуска он не создается заново.
	if err = manager.DeleteURLTest(urlTests[0].ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManager(appData)
	if err != nil {
		t.Fatal(err)
	}

	if got := reloaded.ListURLTests(); len(got) != 0 {
		t.Errorf("urltests after delete = %v", got)
	}

	// После удаления auto тег свободен.
	if _, err = reloaded.Add(map[string]any{"type": "vless", "tag": AutoTag}); err != nil {
		t.Errorf("auto tag must be free after delete: %v", err)
	}
}
