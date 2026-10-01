package handler

import (
	"net/http"
)

// Protect добавляет к ответам заголовки безопасности и отклоняет изменяющие запросы с чужих сайтов (CSRF).
// Без этого любая страница, открытая в браузере пользователя локальной сети, могла бы отправить POST
// в API: при выключенном входе — без всяких ограничений, при включенном — с соседнего порта того же хоста.
// Запросы не из браузера (curl, updater, sing-box) не содержат Origin и Sec-Fetch-Site и пропускаются.
func Protect(next http.Handler) http.Handler {
	csrf := http.NewCrossOriginProtection()

	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusForbidden, "Запрос с другого сайта отклонен")
	}))

	protected := csrf.Handler(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()

		// Панель нельзя встроить во фрейм чужой страницы (clickjacking).
		header.Set("X-Frame-Options", "DENY")
		header.Set("Content-Security-Policy", "frame-ancestors 'none'")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "same-origin")

		protected.ServeHTTP(w, r)
	})
}
