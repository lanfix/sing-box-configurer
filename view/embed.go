// Package view содержит веб-интерфейс конфигуратора (Vue 3) и отдает собранные файлы из dist.
//
// Сборка интерфейса: npm ci && npm run build в каталоге view. Без сборки в dist остается только
// .gitkeep, и сервер отвечает заглушкой.
package view

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// notBuiltPage — ответ, если интерфейс не собран.
const notBuiltPage = `<!doctype html><meta charset="utf-8"><title>Sing-Box Configurer</title>
<p>Веб-интерфейс не собран: выполните <code>npm ci &amp;&amp; npm run build</code> в каталоге view.</p>`

// Handler отдает файлы интерфейса. Пути без файла (маршруты SPA) получают index.html.
// Файлы из assets содержат хеш в имени и кэшируются надолго, index.html — не кэшируется.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServerFS(files)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if name != "" && name != "index.html" {
			if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}

				fileServer.ServeHTTP(w, r)

				return
			}

			// Отсутствующий файл со статикой — 404, а не index.html.
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)

				return
			}
		}

		index, err := fs.ReadFile(files, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(notBuiltPage))

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
