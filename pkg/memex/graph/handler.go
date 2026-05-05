package graph

import (
	"encoding/json"
	"io"
	"net/http"
)

// jsonQueryHandler returns a minimal JSON-over-HTTP query handler. It
// accepts POST /query with a body of `{"dql": "..."}` and returns the
// raw bonsai response body.
//
// This is a portable fallback so embedders importing only pkg/memex/graph
// have a usable HTTP surface without pulling bonsai's full server. Mount
// the umbrella memex.GraphHandler() in cmd/ycode/serve.go for the richer
// Explorer UI when bonsai's HTTP layer is desired.
func jsonQueryHandler(g *Graph) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			DQL  string            `json:"dql"`
			Vars map[string]string `json:"vars,omitempty"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.DQL == "" {
			http.Error(w, "dql field is required", http.StatusBadRequest)
			return
		}
		out, err := g.Query(r.Context(), req.DQL, req.Vars)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	})
	return mux
}
