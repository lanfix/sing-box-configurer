package handler

import (
	"fmt"
	"net/http"
	"time"
)

// Уровни предупреждений о подписках.
const (
	alertWarn = "warn"
	alertBad  = "bad"
)

// amneziaDateLayouts — форматы дат шлюза Amnezia ("2026-10-06 12:54:28+00:00").
var amneziaDateLayouts = []string{"2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05Z07:00", time.RFC3339}

// subscriptionAlert — предупреждение о подписке, которая заканчивается.
type subscriptionAlert struct {
	Source  string `json:"source"`
	Name    string `json:"name"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// expiryAlert возвращает предупреждение, если до окончания срока меньше недели.
func expiryAlert(source, name, what string, expire time.Time) *subscriptionAlert {
	days := int(time.Until(expire).Hours() / 24)

	alert := &subscriptionAlert{
		Source: source,
		Name:   name,
		Level:  alertWarn,
	}

	switch {
	case time.Until(expire) < 0:
		alert.Level = alertBad
		alert.Message = fmt.Sprintf("%s: %s истекла", name, what)

	case days < 3:
		alert.Level = alertBad
		alert.Message = fmt.Sprintf("%s: %s заканчивается через %d дн.", name, what, days)

	case days < 7:
		alert.Message = fmt.Sprintf("%s: %s заканчивается через %d дн.", name, what, days)

	default:
		return nil
	}

	return alert
}

// parseAmneziaDate разбирает дату шлюза Amnezia.
func parseAmneziaDate(value string) (time.Time, bool) {
	for _, layout := range amneziaDateLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}

// GetSubscriptionAlerts возвращает предупреждения о подписках Happ и Amnezia Premium, у которых
// заканчивается срок действия или трафик.
func (h *Handler) GetSubscriptionAlerts(w http.ResponseWriter, _ *http.Request) {
	alerts := make([]subscriptionAlert, 0)

	for _, profile := range h.happManager.List() {
		if profile.Info == nil {
			continue
		}

		if profile.Info.Expire != nil {
			if alert := expiryAlert("happ", profile.Name, "подписка", *profile.Info.Expire); alert != nil {
				alerts = append(alerts, *alert)
			}
		}

		if profile.Info.Total <= 0 {
			continue
		}

		used := float64(profile.Info.Upload+profile.Info.Download) / float64(profile.Info.Total)

		if used >= 0.75 {
			level := alertWarn

			if used >= 0.9 {
				level = alertBad
			}

			alerts = append(alerts, subscriptionAlert{
				Source:  "happ",
				Name:    profile.Name,
				Level:   level,
				Message: fmt.Sprintf("%s: израсходовано %.0f%% трафика", profile.Name, used*100),
			})
		}
	}

	for _, profile := range h.amneziaManager.List() {
		if profile.Premium == nil {
			continue
		}

		dates := []struct {
			value string
			what  string
		}{
			{value: profile.Premium.SubscriptionEnd, what: "подписка"},
			{value: profile.Premium.ConfigExpiresAt, what: "конфигурация"},
		}

		for _, date := range dates {
			expire, ok := parseAmneziaDate(date.value)
			if !ok {
				continue
			}

			if alert := expiryAlert("amnezia", profile.Name, date.what, expire); alert != nil {
				alerts = append(alerts, *alert)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": alerts,
	})
}
