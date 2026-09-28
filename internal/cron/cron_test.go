package cron

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	base := time.Date(2026, 9, 28, 20, 54, 0, 0, time.UTC) // понедельник

	cases := map[string]time.Time{
		"0 6 * * *":       time.Date(2026, 9, 29, 6, 0, 0, 0, time.UTC),
		"@daily":          time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
		"*/15 * * * *":    time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC),
		"30 4 * * 0":      time.Date(2026, 10, 4, 4, 30, 0, 0, time.UTC),
		"30 4 * * 7":      time.Date(2026, 10, 4, 4, 30, 0, 0, time.UTC),
		"0 3 1,15 * *":    time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC),
		"0 22-23/1 * * *": time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC),
		"0 0 1 * 1":       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), // день месяца или понедельник
	}

	for expr, want := range cases {
		schedule, err := Parse(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}

		if got := schedule.Next(base); !got.Equal(want) {
			t.Errorf("%s: next = %v, want %v", expr, got, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "5-1 * * * *", "a * * * *"} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("%q: expected error", expr)
		}
	}

	schedule, err := Parse("0 0 30 2 *")
	if err != nil {
		t.Fatal(err)
	}

	if !schedule.Next(time.Now()).IsZero() {
		t.Error("impossible schedule must have no next run")
	}
}

func TestNextInTimezone(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*60*60)
	schedule, _ := Parse("0 6 * * *")

	// 20:54 UTC — это 23:54 по Москве: следующий запуск в 06:00 по Москве, то есть в 03:00 UTC.
	next := schedule.Next(time.Date(2026, 9, 28, 20, 54, 0, 0, time.UTC).In(moscow))

	if want := time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Errorf("next = %v, want %v", next, want)
	}
}
