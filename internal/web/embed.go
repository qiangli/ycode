package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded web UI.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("web: embedded static files not found: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
