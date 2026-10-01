package singbox

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
)

// parseJSON разбирает JSON-объект теста.
func parseJSON(t *testing.T, raw string) map[string]any {
	t.Helper()

	var result map[string]any

	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}

	return result
}

// findOutbound возвращает outbound конфига по тегу.
func findOutbound(config map[string]any, tag string) map[string]any {
	items, _ := config["outbounds"].([]any)

	for _, item := range items {
		if object, ok := item.(map[string]any); ok && jsonmap.String(object, "tag") == tag {
			return object
		}
	}

	return nil
}

// outboundTags возвращает теги outbound-ов конфига по порядку.
func outboundTags(config map[string]any) []string {
	items, _ := config["outbounds"].([]any)
	tags := make([]string, 0, len(items))

	for _, item := range items {
		tags = append(tags, jsonmap.String(item.(map[string]any), "tag"))
	}

	return tags
}

// TestPatchSubscriptions проверяет, что в рабочий конфиг переносятся только изменения серверов подписки:
// неприменённый manual-2 и неприменённый порядок участников select-work туда не попадают.
func TestPatchSubscriptions(t *testing.T) {
	actual := parseJSON(t, `{"outbounds": [
		{"type": "vless", "tag": "manual-1", "server": "m1"},
		{"type": "vless", "tag": "happ-a", "server": "old-a"},
		{"type": "vless", "tag": "happ-b", "server": "b"},
		{"type": "urltest", "tag": "auto", "outbounds": ["happ-a", "happ-b"]},
		{"type": "direct", "tag": "direct"},
		{"type": "block", "tag": "block"},
		{"type": "selector", "tag": "select-default", "outbounds": ["auto", "manual-1", "happ-a", "happ-b", "direct", "block"], "default": "happ-b"},
		{"type": "selector", "tag": "select-work", "outbounds": ["manual-1", "direct", "block"], "default": "manual-1"}
	], "route": {"final": "select-default"}}`)

	rendered := parseJSON(t, `{"outbounds": [
		{"type": "vless", "tag": "manual-1", "server": "m1"},
		{"type": "vless", "tag": "manual-2", "server": "m2"},
		{"type": "vless", "tag": "happ-a", "server": "new-a"},
		{"type": "vless", "tag": "happ-c", "server": "c"},
		{"type": "urltest", "tag": "auto", "outbounds": ["happ-a", "happ-c"]},
		{"type": "direct", "tag": "direct"},
		{"type": "block", "tag": "block"},
		{"type": "selector", "tag": "select-default", "outbounds": ["auto", "manual-1", "manual-2", "happ-a", "happ-c", "direct", "block"], "default": "auto"},
		{"type": "selector", "tag": "select-work", "outbounds": ["direct", "manual-1", "manual-2", "block"], "default": "manual-2"}
	], "route": {"final": "select-work"}}`)

	if err := patchSubscriptions(actual, rendered, []string{"happ-a", "happ-b"}, []string{"happ-a", "happ-c"}); err != nil {
		t.Fatal(err)
	}

	wantTags := []string{"manual-1", "happ-a", "happ-c", "auto", "direct", "block", "select-default", "select-work"}

	if tags := outboundTags(actual); !slices.Equal(tags, wantTags) {
		t.Errorf("tags = %v, want %v", tags, wantTags)
	}

	if server := jsonmap.String(findOutbound(actual, "happ-a"), "server"); server != "new-a" {
		t.Errorf("happ-a server = %q, want new-a", server)
	}

	if members := jsonmap.Strings(findOutbound(actual, "auto"), "outbounds"); !slices.Equal(members, []string{"happ-a", "happ-c"}) {
		t.Errorf("auto members = %v", members)
	}

	selector := findOutbound(actual, "select-default")

	if members := jsonmap.Strings(selector, "outbounds"); !slices.Equal(members, []string{"auto", "manual-1", "happ-a", "happ-c", "direct", "block"}) {
		t.Errorf("select-default members = %v", members)
	}

	// Выбранный по умолчанию сервер удален — берется выбор из отрендеренного конфига.
	if value := jsonmap.String(selector, "default"); value != "auto" {
		t.Errorf("select-default default = %q, want auto", value)
	}

	work := findOutbound(actual, "select-work")

	if members := jsonmap.Strings(work, "outbounds"); !slices.Equal(members, []string{"manual-1", "direct", "block"}) || jsonmap.String(work, "default") != "manual-1" {
		t.Errorf("select-work must stay untouched: %v", work)
	}

	if final := jsonmap.String(actual["route"].(map[string]any), "final"); final != "select-default" {
		t.Errorf("route.final = %q, pending change must not be applied", final)
	}
}

// TestPatchSubscriptionsEmptyGroup проверяет, что urltest без участников не записывается в рабочий конфиг.
func TestPatchSubscriptionsEmptyGroup(t *testing.T) {
	actual := parseJSON(t, `{"outbounds": [
		{"type": "vless", "tag": "happ-a"},
		{"type": "urltest", "tag": "auto", "outbounds": ["happ-a"]}
	]}`)

	rendered := parseJSON(t, `{"outbounds": []}`)

	if err := patchSubscriptions(actual, rendered, []string{"happ-a"}, nil); err == nil {
		t.Error("empty urltest must be rejected")
	}
}

// TestMergeMembers проверяет порядок участников после обновления.
func TestMergeMembers(t *testing.T) {
	members := mergeMembers(
		[]string{"x", "old", "y"},
		[]string{"pending", "x", "new", "y"},
		map[string]bool{"old": true},
		map[string]bool{"new": true},
	)

	if !slices.Equal(members, []string{"x", "new", "y"}) {
		t.Errorf("members = %v", members)
	}
}
