package jsonmap

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	dst := map[string]any{
		"type": "https",
		"tls": map[string]any{
			"server_name": "a",
		},
	}

	src := map[string]any{
		"tls": map[string]any{
			"insecure": true,
		},
		"neighbor_domain": []any{"."},
	}

	Merge(dst, src)

	want := map[string]any{
		"type": "https",
		"tls": map[string]any{
			"server_name": "a",
			"insecure":    true,
		},
		"neighbor_domain": []any{"."},
	}

	if !reflect.DeepEqual(dst, want) {
		t.Errorf("Merge = %v, want %v", dst, want)
	}

	// Изменение src не должно влиять на dst.
	src["neighbor_domain"].([]any)[0] = "lan"

	if dst["neighbor_domain"].([]any)[0] != "." {
		t.Error("Merge must copy values")
	}
}

func TestClone(t *testing.T) {
	src := map[string]any{
		"list": []any{map[string]any{"a": 1.0}},
	}

	cloned := Clone(src)
	cloned["list"].([]any)[0].(map[string]any)["a"] = 2.0

	if src["list"].([]any)[0].(map[string]any)["a"] != 1.0 {
		t.Error("Clone must be deep")
	}
}
