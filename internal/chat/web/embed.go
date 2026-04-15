// Package web provides the embedded static assets for the chat web UI.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded chat UI.
// Non-file paths fall back to index.html for SPA routing.
func Handler() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try serving the file directly.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := sub.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback — serve index.html.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
