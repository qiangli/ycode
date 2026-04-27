package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Embed(t *testing.T) {
	// Mock Ollama /api/embed endpoint.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Return a mock embedding.
		resp := map[string]any{
			"model":      req.Model,
			"embeddings": [][]float64{{0.1, 0.2, 0.3, 0.4}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Model:   "nomic-embed-text",
		Dims:    4,
	})

	embedding, err := provider.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 4 {
		t.Fatalf("expected 4 dimensions, got %d", len(embedding))
	}

	// Verify values.
	expected := []float32{0.1, 0.2, 0.3, 0.4}
	for i, v := range embedding {
		if v != expected[i] {
			t.Errorf("embedding[%d] = %f, want %f", i, v, expected[i])
		}
	}
}

func TestOllamaProvider_Dimensions(t *testing.T) {
	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
		Dims:    768,
	})

	if provider.Dimensions() != 768 {
		t.Errorf("expected 768 dimensions, got %d", provider.Dimensions())
	}
}

func TestOllamaProvider_DefaultDimensions(t *testing.T) {
	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
	})

	if provider.Dimensions() != 768 {
		t.Errorf("expected default 768 dimensions, got %d", provider.Dimensions())
	}
}

func TestOllamaProvider_EmbedServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Model:   "nonexistent",
		Dims:    4,
	})

	_, err := provider.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestOllamaProvider_EmbedEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model":      "test",
			"embeddings": [][]float64{},
		})
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Model:   "test",
		Dims:    4,
	})

	_, err := provider.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for empty embedding response")
	}
}

func TestOllamaProvider_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model":      "test",
			"embeddings": [][]float64{{0.1}},
		})
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Model:   "test",
		Dims:    1,
	})

	if !provider.Healthy(context.Background()) {
		t.Error("should be healthy when server responds")
	}
}

func TestOllamaProvider_UnhealthyWhenDown(t *testing.T) {
	// Use a non-existent server address.
	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://127.0.0.1:0", // port 0 — nothing listening
		Model:   "test",
		Dims:    1,
	})

	if provider.Healthy(context.Background()) {
		t.Error("should not be healthy when server is unreachable")
	}
}

func TestDetectOllamaEmbedding_ReturnsNilWhenDown(t *testing.T) {
	// Use a non-existent server.
	result := DetectOllamaEmbedding("http://127.0.0.1:0", "test")
	if result != nil {
		t.Error("should return nil when Ollama is not reachable")
	}
}
