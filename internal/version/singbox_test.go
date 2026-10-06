package version

import "testing"

func TestCompareSingBox(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v1.14.1-lx.8", "v1.14.2-lx.11-mac.1", -1, true},
		{"1.14.2-lx.11", "v1.14.2-lx.11-mac.1", -1, true},
		{"v1.14.2-lx.11-mac.1", "v1.14.2-lx.11-mac.1", 0, true},
		{"v1.14.2-lx.12", "v1.14.2-lx.11-mac.1", 1, true},
		{"v1.15.0-lx.1", "v1.14.2-lx.11-mac.1", 1, true},
		{"v1.14.2", "v1.14.2-lx.11", 0, false},
		{"latest", "v1.14.2-lx.11", 0, false},
		{"v1.14.2-lx.11-mac", "v1.14.2-lx.11", 0, false},
	}

	for _, c := range cases {
		got, ok := CompareSingBox(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("CompareSingBox(%q, %q) = %d %v, want %d %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}
