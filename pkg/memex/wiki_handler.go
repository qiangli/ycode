package memex

import (
	"encoding/json"
	"net/http"
)

// WikiHandler exposes the memex VFS over HTTP. Mountable at any prefix
// (typically combined with the memos handler under /memos/api/wiki/ via
// Memex.HTTPHandler).
//
// Routes:
//
//	GET /tree?path=/path/         List entries under a virtual directory.
//	                              Returns []Node as JSON.
//	GET /file?path=/path/x.md     Read one virtual file.
//	                              Returns {body, node} as JSON.
//
// Both endpoints are read-only by design — the wiki UI's writes flow
// through the underlying memory and memos REST APIs, not through this
// overlay.
func WikiHandler(v VFS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/tree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		nodes, err := v.List(r.Context(), path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, nodes)
	})

	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "path query parameter required", http.StatusBadRequest)
			return
		}
		body, node, err := v.Read(r.Context(), path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, struct {
			Body string `json:"body"`
			Node Node   `json:"node"`
		}{Body: string(body), Node: node})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
