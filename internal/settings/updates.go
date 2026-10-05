package settings

import (
	"fmt"
	"slices"
	"time"
)

// UpdateCheckIntervals — допустимые интервалы автоматической проверки обновлений, ч.
var UpdateCheckIntervals = []int{1, 3, 6, 12, 24}

// defaultUpdateCheckInterval — интервал автоматической проверки обновлений по умолчанию, ч.
const defaultUpdateCheckInterval = 6

// Updates — автоматическая проверка обновлений конфигуратора.
type Updates struct {
	// AutoCheck — конфигуратор сам проверяет новые версии с интервалом IntervalHours.
	AutoCheck bool `json:"auto_check"`

	// IntervalHours — интервал проверки, ч.
	IntervalHours int `json:"interval_hours"`
}

// DefaultUpdates возвращает настройки проверки обновлений по умолчанию: автоматическая проверка выключена.
func DefaultUpdates() Updates {
	return Updates{
		AutoCheck:     false,
		IntervalHours: defaultUpdateCheckInterval,
	}
}

// Interval возвращает интервал автоматической проверки.
func (u Updates) Interval() time.Duration {
	return time.Duration(u.IntervalHours) * time.Hour
}

// Validate проверяет интервал автоматической проверки.
func (u Updates) Validate() error {
	if !slices.Contains(UpdateCheckIntervals, u.IntervalHours) {
		return fmt.Errorf("интервал проверки обновлений — один из %v ч", UpdateCheckIntervals)
	}

	return nil
}
