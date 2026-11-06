package api

import (
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

func init() {
	// манифест PWA: без правильного типа часть браузеров не предлагает «Установить приложение»
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// spa раздаёт собранный фронтенд; неизвестные пути отдают index.html (клиентский роутинг).
func (s *Server) spa() http.Handler {
	files := http.FileServer(http.FS(s.Static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if strings.HasPrefix(p, "r/") {
			// реферальная ссылка autoinvest.by/r/CODE → регистрация с кодом
			http.Redirect(w, r, "/register?ref="+url.QueryEscape(strings.TrimPrefix(p, "r/")), http.StatusFound)
			return
		}
		if p != "" {
			if f, err := s.Static.Open(p); err == nil {
				st, _ := f.Stat()
				f.Close()
				if st != nil && !st.IsDir() {
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		b, err := fs.ReadFile(s.Static, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	})
}
