// Package views renders presentation only. Effects stay behind the GUI bridge.
package views

import (
	"embed"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var resources embed.FS

func Handler(locale i18n.Locale) http.Handler {
	local, _ := fs.Sub(resources, "assets")
	files := http.StripPrefix("/assets/", http.FileServer(http.FS(local)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := Layout(i18n.Catalog(locale), string(locale)).Render(r.Context(), w); err != nil {
				return
			}
			return
		}
		if len(r.URL.Path) >= 8 && r.URL.Path[:8] == "/assets/" {
			files.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}
