// Package cron разбирает расписания в формате crontab (минута, час, день месяца, месяц, день недели)
// и вычисляет время следующего запуска.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// maxSearch ограничивает поиск следующего запуска: расписание вроде 30 февраля не сработает никогда.
const maxSearch = 366 * 24 * time.Hour

// aliases — сокращенные расписания.
var aliases = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// field описывает поле расписания: допустимый диапазон и название для ошибок.
type field struct {
	name string
	min  int
	max  int
}

var fields = []field{
	{name: "минута", min: 0, max: 59},
	{name: "час", min: 0, max: 23},
	{name: "день месяца", min: 1, max: 31},
	{name: "месяц", min: 1, max: 12},
	{name: "день недели", min: 0, max: 7},
}

// Schedule — разобранное расписание.
type Schedule struct {
	minutes  []bool
	hours    []bool
	days     []bool
	months   []bool
	weekdays []bool

	// Ограничены ли день месяца и день недели: если оба, срабатывает любое из условий (как в crontab).
	daysRestricted     bool
	weekdaysRestricted bool
}

// Parse разбирает расписание в формате crontab.
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)

	if alias, ok := aliases[strings.ToLower(expr)]; ok {
		expr = alias
	}

	parts := strings.Fields(expr)
	if len(parts) != len(fields) {
		return nil, fmt.Errorf("расписание должно состоять из 5 полей (минута час день месяц день_недели), получено %d", len(parts))
	}

	sets := make([][]bool, len(fields))

	for i, part := range parts {
		set, err := parseField(part, fields[i])
		if err != nil {
			return nil, err
		}

		sets[i] = set
	}

	// Воскресенье — и 0, и 7.
	if sets[4][7] {
		sets[4][0] = true
	}

	return &Schedule{
		minutes:            sets[0],
		hours:              sets[1],
		days:               sets[2],
		months:             sets[3],
		weekdays:           sets[4],
		daysRestricted:     parts[2] != "*",
		weekdaysRestricted: parts[4] != "*",
	}, nil
}

// parseField разбирает одно поле: *, числа, диапазоны a-b и шаги */n или a-b/n через запятую.
func parseField(value string, f field) ([]bool, error) {
	set := make([]bool, f.max+1)

	for _, item := range strings.Split(value, ",") {
		rangePart, stepPart, hasStep := strings.Cut(item, "/")

		step := 1

		if hasStep {
			parsed, err := strconv.Atoi(stepPart)
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("поле «%s»: некорректный шаг %q", f.name, stepPart)
			}

			step = parsed
		}

		from, to := f.min, f.max

		switch {
		case rangePart == "*":

		case strings.Contains(rangePart, "-"):
			start, end, _ := strings.Cut(rangePart, "-")

			var err error

			if from, err = parseNumber(start, f); err != nil {
				return nil, err
			}

			if to, err = parseNumber(end, f); err != nil {
				return nil, err
			}

			if from > to {
				return nil, fmt.Errorf("поле «%s»: начало диапазона %d больше конца %d", f.name, from, to)
			}

		default:
			number, err := parseNumber(rangePart, f)
			if err != nil {
				return nil, err
			}

			from = number

			// Одиночное число с шагом (5/15) означает «с 5 до конца с шагом 15».
			if !hasStep {
				to = number
			}
		}

		for i := from; i <= to; i += step {
			set[i] = true
		}
	}

	return set, nil
}

// parseNumber разбирает число и проверяет, что оно в допустимом диапазоне поля.
func parseNumber(value string, f field) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("поле «%s»: некорректное значение %q", f.name, value)
	}

	if number < f.min || number > f.max {
		return 0, fmt.Errorf("поле «%s»: значение %d вне диапазона %d-%d", f.name, number, f.min, f.max)
	}

	return number, nil
}

// Matches проверяет, что расписание срабатывает в минуту t (в часовом поясе t).
func (s *Schedule) Matches(t time.Time) bool {
	if !s.minutes[t.Minute()] || !s.hours[t.Hour()] || !s.months[int(t.Month())] {
		return false
	}

	dayMatch := s.days[t.Day()]
	weekdayMatch := s.weekdays[int(t.Weekday())]

	if s.daysRestricted && s.weekdaysRestricted {
		return dayMatch || weekdayMatch
	}

	return dayMatch && weekdayMatch
}

// Next возвращает время следующего срабатывания строго после after (в часовом поясе after).
// Если за год срабатываний нет, возвращает нулевое время.
func (s *Schedule) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	deadline := after.Add(maxSearch)

	for t.Before(deadline) {
		if s.Matches(t) {
			return t
		}

		t = t.Add(time.Minute)
	}

	return time.Time{}
}
