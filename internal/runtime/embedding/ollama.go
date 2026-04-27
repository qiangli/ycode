package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaProvider generates embeddings using a local Ollama instance.
// Uses Ollama's native /api/embed endpoint (not OpenAI-compatible).
type OllamaProvider struct {
	baseURL string
	model   string
	dims    int
	client  *http.Client
}

// OllamaConfig configures an Ollama embedding provider.
type OllamaConfig struct {
	BaseURL string       // e.g., "http://127.0.0.1:11434"
	Model   string       // e.g., "nomic-embed-text"
	Dims    int          // expected output dimensions
	Client  *http.Client // optional HTTP client
}

// NewOllamaProvider creates an Ollama-based embedding provider.
func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	if cfg.Dims <= 0 {
		cfg.Dims = 768 // nomic-embed-text default
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &OllamaProvider{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		dims:    cfg.Dims,
		client:  client,
	}
}

// Embed generates an embedding vector via Ollama's /api/embed endpoint.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": p.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed error %d: %s", resp.StatusCode, string(respBody))
	}

	// Ollama /api/embed response format:
	// {"model":"nomic-embed-text","embeddings":[[0.1,0.2,...]]}
	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama embed response: %w", err)
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned from ollama")
	}

	// Convert float64 to float32.
	embedding := make([]float32, len(result.Embeddings[0]))
	for i, v := range result.Embeddings[0] {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// Dimensions returns the expected vector dimensionality.
func (p *OllamaProvider) Dimensions() int {
	return p.dims
}

// Healthy checks if the Ollama instance is reachable and the embedding model
// is available. Uses a 2-second timeout.
func (p *OllamaProvider) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Quick health check — try to embed a single word.
	_, err := p.Embed(ctx, "test")
	return err == nil
}

// DetectOllamaEmbedding checks if a local Ollama instance is available and
// has an embedding model loaded. Returns a provider or nil.
func DetectOllamaEmbedding(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = defaultOllamaURL()
	}
	if model == "" {
		model = "nomic-embed-text"
	}

	provider := NewOllamaProvider(OllamaConfig{
		BaseURL: baseURL,
		Model:   model,
		Dims:    768,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if provider.Healthy(ctx) {
		return provider
	}
	return nil
}

// defaultOllamaURL returns the default Ollama base URL from env or fallback.
func defaultOllamaURL() string {
	// Check OLLAMA_HOST first (same as Ollama CLI).
	// Not importing os.Getenv here to keep it simple; callers should pass it.
	return "http://127.0.0.1:11434"
}
