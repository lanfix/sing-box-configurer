package handler

import (
	"net/http"

	"github.com/lanfix/sing-box-configurer/internal/migrations"
	"github.com/lanfix/sing-box-configurer/internal/version"
)

// Health возвращает обработчик /api/health. Updater по нему проверяет, что новая версия
// запустилась и миграции прошли: сервер начинает слушать порт только после миграций.
// Доступен без входа в панель.
func Health(migrationResult *migrations.Result, platform string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"version":        version.Version,
			"platform":       platform,
			"schema_version": migrationResult.ToVersion,
			"migrations":     migrationResult,
		})
	}
}
