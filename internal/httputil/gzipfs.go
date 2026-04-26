// Package httputil provides HTTP utilities for serving embedded assets.
package httputil

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// GzipFileServer serves files from an fs.FS where all files have been
// pre-gzipped (stored with .gz extension). When the client accepts gzip,
// the compressed bytes are sent directly. Otherwise, files are decompressed
// on the fly. Requests use the original filename (without .gz suffix).
func GzipFileServer(fsys fs.FS) http.Handler {
	return &gzipFileHandler{fs: fsys}
}

type gzipFileHandler struct {
	fs fs.FS
}

func (h *gzipFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "." {
		p = "index.html"
	}

	// All embedded files are stored with .gz extension.
	gzPath := p + ".gz"
	data, err := fs.ReadFile(h.fs, gzPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ct := contentTypeFor(p)
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	if acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Write(data)
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer gz.Close()
		io.Copy(w, gz)
	}
}

// ReadGzipFile reads and decompresses a single .gz file from the FS.
// The name should be the original filename (without .gz suffix).
func ReadGzipFile(fsys fs.FS, name string) ([]byte, error) {
	data, err := fs.ReadFile(fsys, name+".gz")
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript"
	case strings.HasSuffix(name, ".css"):
		return "text/css"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".webp"):
		return "image/webp"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".ttf"):
		return "font/ttf"
	case strings.HasSuffix(name, ".woff"):
		return "font/woff"
	case strings.HasSuffix(name, ".webmanifest"):
		return "application/manifest+json"
	default:
		return "application/octet-stream"
	}
}
