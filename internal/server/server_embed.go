package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/qiangli/ycode/internal/runtime/embedding"
)

// embedRequest is the wire shape for POST /api/embed.
type embedRequest struct {
	Text string `json:"text"`
}

type embedResponse struct {
	Vector     []float32 `json:"vector"`
	Dimensions int       `json:"dimensions"`
}

// embedBatchRequest is the wire shape for POST /api/embed/batch.
type embedBatchRequest struct {
	Texts []string `json:"texts"`
}

type embedBatchResponse struct {
	Vectors    [][]float32 `json:"vectors"`
	Dimensions int         `json:"dimensions"`
}

type embedDimensionsResponse struct {
	Dimensions int `json:"dimensions"`
}

// getEmbedProvider lazily initializes the embedding provider for this
// server. Tests inject their own via setEmbedProviderForTest.
func (s *Server) getEmbedProvider() embeddingProvider {
	s.embedOnce.Do(func() {
		if s.embedProv == nil {
			s.embedProv = embedding.DetectProvider()
		}
	})
	return s.embedProv
}

// setEmbedProviderForTest installs a deterministic embedding provider so
// tests don't depend on env vars or filesystem state. Test-only.
func (s *Server) setEmbedProviderForTest(p embeddingProvider) {
	s.embedOnce.Do(func() {
		s.embedProv = p
	})
}

// handleEmbed returns a single embedding for the given text.
func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	var body embedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("text required"))
		return
	}
	p := s.getEmbedProvider()
	if p == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("no embedding provider available"))
		return
	}
	vec, err := p.Embed(r.Context(), body.Text)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("embed: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, embedResponse{Vector: vec, Dimensions: p.Dimensions()})
}

// handleEmbedBatch returns embeddings for a slice of texts. The current
// embedding.Provider implementation issues sequential calls — no native
// batch support yet — but the API contract is batched so callers can
// migrate without changes when batching arrives.
func (s *Server) handleEmbedBatch(w http.ResponseWriter, r *http.Request) {
	var body embedBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	if len(body.Texts) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("texts required"))
		return
	}
	p := s.getEmbedProvider()
	if p == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("no embedding provider available"))
		return
	}
	out := make([][]float32, len(body.Texts))
	for i, t := range body.Texts {
		v, err := p.Embed(r.Context(), t)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("embed[%d]: %w", i, err))
			return
		}
		out[i] = v
	}
	writeJSON(w, http.StatusOK, embedBatchResponse{Vectors: out, Dimensions: p.Dimensions()})
}

// handleEmbedDimensions reports the active provider's vector dimensionality.
// Useful for clients allocating storage or validating other vectors against
// a known size before sending them upstream.
func (s *Server) handleEmbedDimensions(w http.ResponseWriter, r *http.Request) {
	p := s.getEmbedProvider()
	if p == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("no embedding provider available"))
		return
	}
	writeJSON(w, http.StatusOK, embedDimensionsResponse{Dimensions: p.Dimensions()})
}
