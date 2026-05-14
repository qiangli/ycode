package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded web UI
// without any token injection. Useful for tests and for builds where
// the SPA must not learn the server token.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("web: embedded static files not found: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}

// HandlerWithToken returns an http.Handler that serves the embedded
// SPA, additionally injecting `<script>window.YCODE_TOKEN="…"</script>`
// into the HTML responses for index pages (/, /index.html,
// /canvas/index.html). The SPAs (chat at /, canvas at /canvas/) then
// fall back to window.YCODE_TOKEN when the visiting URL has no
// ?token= query — so a human clicking the canvas tile on the proxy
// landing page reaches a working session without manually pasting a
// token.
//
// Posture: the token is leaked to whoever can fetch the page. ycode
// binds 127.0.0.1 by default, and any local-uid process already has
// read access to ~/.agents/ycode/server.token — so this is no looser
// than the existing local posture. For non-localhost deployments,
// pair the SPA with a reverse proxy that requires auth before
// reaching /; the canvas/chat HTML is unauthed by design today.
//
// token="" → no injection, identical to Handler().
func HandlerWithToken(token string) http.Handler {
	fileServer := Handler()
	if token == "" {
		return fileServer
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isIndexPath(r.URL.Path) {
			fileServer.ServeHTTP(w, r)
			return
		}
		fsPath := resolveIndexPath(r.URL.Path)
		data, err := staticFS.ReadFile("static/" + fsPath)
		if err != nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(injectToken(data, token))
	})
}

// isIndexPath reports whether the request path resolves to an
// embedded index.html that should receive token injection.
func isIndexPath(p string) bool {
	if p == "/" || p == "/index.html" {
		return true
	}
	if strings.HasSuffix(p, "/index.html") {
		return true
	}
	// Directory-style request like /canvas/ — FileServer would serve
	// /canvas/index.html. Match the same shape.
	return strings.HasSuffix(p, "/")
}

// resolveIndexPath maps a URL path to the embed-relative file name
// that FileServer would have served.
func resolveIndexPath(p string) string {
	trimmed := strings.TrimPrefix(p, "/")
	if trimmed == "" || strings.HasSuffix(trimmed, "/") {
		trimmed += "index.html"
	}
	return trimmed
}

// injectToken splices `<script>window.YCODE_TOKEN="…"</script>` into
// the page just after `</head>` (or at the end of the body if there
// is no head tag). JSON-encodes the token so embedded quotes/slashes
// can't break out of the literal.
func injectToken(data []byte, token string) []byte {
	encoded, err := json.Marshal(token)
	if err != nil {
		return data
	}
	script := []byte(`<script>window.YCODE_TOKEN=` + string(encoded) + `;</script>`)
	if idx := bytes.Index(data, []byte("</head>")); idx >= 0 {
		out := make([]byte, 0, len(data)+len(script))
		out = append(out, data[:idx]...)
		out = append(out, script...)
		out = append(out, data[idx:]...)
		return out
	}
	// No </head> — append the script at the end. Browsers tolerate
	// trailing <script> outside <body>, and either way the global is
	// set before the SPA's own scripts read it (we only ever inject
	// into HTML that has its own scripts after this point).
	out := make([]byte, 0, len(data)+len(script))
	out = append(out, data...)
	out = append(out, script...)
	return out
}
