package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/qiangli/ycode/internal/runtime/extract"
)

// extractRequest is the wire shape for POST /api/extract.
type extractRequest struct {
	Model      string          `json:"model,omitempty"`
	MaxTokens  int             `json:"max_tokens,omitempty"`
	System     string          `json:"system,omitempty"`
	Schema     json.RawMessage `json:"schema,omitempty"`
	SchemaName string          `json:"schema_name,omitempty"`
	Prompt     string          `json:"prompt"`
}

// handleExtract runs a single non-agentic LLM call constrained to JSON
// output and returns the raw bytes the model produced.
//
// workDir is required so the handler can resolve the per-tenant App and
// reach its provider/config defaults. The Bearer token + actor headers are
// already validated by the auth middleware before this handler runs.
func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	workDir := workDirFromRequest(r)
	if workDir == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("work_dir required (header X-Work-Dir, query ?work_dir=, or body field)"))
		return
	}

	var body extractRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	if body.Prompt == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("prompt required"))
		return
	}

	app, err := s.service.LookupApp(r.Context(), workDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	provider := app.Provider()
	if provider == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("no LLM provider configured (set ANTHROPIC_API_KEY, OPENAI_API_KEY, or use Ollama)"))
		return
	}
	cfg := app.Config()

	model := body.Model
	if model == "" {
		model = cfg.Model
	}
	maxTokens := body.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}

	out, err := extract.Run(r.Context(), provider, extract.Options{
		Model:      model,
		MaxTokens:  maxTokens,
		System:     body.System,
		Schema:     body.Schema,
		SchemaName: body.SchemaName,
	}, body.Prompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Return the raw model bytes verbatim — the model already produced
	// JSON, so don't re-marshal (which would double-encode).
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// workDirFromRequest resolves the workDir from (in order) the X-Work-Dir
// header, the ?work_dir= query parameter, or a JSON body field — but NOT
// the body, because some handlers want to peek at the body itself first.
// For body-aware handlers, the caller must read it from the parsed body.
func workDirFromRequest(r *http.Request) string {
	if v := r.Header.Get("X-Work-Dir"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("work_dir"); v != "" {
		return v
	}
	return ""
}
