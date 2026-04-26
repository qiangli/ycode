// Package memosweb provides the embedded Memos frontend assets and an HTTP
// handler that serves them. The assets are a modified build of the Memos
// frontend (https://github.com/usememos/memos, MIT license) configured
// for serving under the /memos/ URL subpath.
//
// Assets are pre-gzipped at build time for smaller binary size. The handler
// serves them with Content-Encoding: gzip when the client supports it, or
// decompresses on the fly otherwise.
//
// See external/memos/NOTICE for attribution details.
package memosweb

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/qiangli/ycode/internal/httputil"
)

//go:embed dist/*
var embeddedFiles embed.FS

// csp is the Content-Security-Policy for the Memos UI.
// It blocks all external network access (air-gapped environment support).
const csp = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"media-src 'self' blob:; " +
	"worker-src 'self' blob:; " +
	"frame-src 'none'"

// Handler returns an http.Handler that serves the embedded Memos frontend.
// For paths matching a file in the embedded FS, the file is served directly.
// All other paths get index.html (SPA HTML5 history mode fallback).
func Handler() http.Handler {
	sub, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		panic(err)
	}
	gzipServer := httputil.GzipFileServer(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if the gzipped file exists in the embedded FS.
		if _, err := fs.Stat(sub, path+".gz"); err == nil {
			// Set cache headers: immutable for hashed assets, no-cache for HTML.
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
			} else if strings.HasSuffix(path, ".html") || path == "index.html" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			}
			gzipServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for unmatched paths.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, _ := httputil.ReadGzipFile(sub, "index.html")
		w.Write(data)
	})
}
