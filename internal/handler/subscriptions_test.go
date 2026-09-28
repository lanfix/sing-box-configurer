package handler

import (
	"testing"
	"time"
)

func TestExpiryAlert(t *testing.T) {
	cases := []struct {
		expire time.Time
		level  string
	}{
		{expire: time.Now().Add(-time.Hour), level: alertBad},
		{expire: time.Now().Add(48 * time.Hour), level: alertBad},
		{expire: time.Now().Add(5 * 24 * time.Hour), level: alertWarn},
		{expire: time.Now().Add(30 * 24 * time.Hour), level: ""},
	}

	for _, c := range cases {
		alert := expiryAlert("happ", "P", "подписка", c.expire)

		level := ""

		if alert != nil {
			level = alert.Level
		}

		if level != c.level {
			t.Errorf("expire %v: level %q, want %q", c.expire, level, c.level)
		}
	}

	if _, ok := parseAmneziaDate("2026-10-06 12:54:28+00:00"); !ok {
		t.Error("amnezia gateway date must be parsed")
	}
}
