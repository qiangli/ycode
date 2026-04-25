package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// APIProvider generates embeddings using an API endpoint.
// It supports OpenAI-compatible embedding APIs.
type APIProvider struct {
	baseURL string
	apiKey  string
	model   string
	dims    int
	client  *http.Client
}

// APIConfig configures an API embedding provider.
type APIConfig struct {
	BaseURL string // e.g., "https://api.openai.com/v1"
	APIKey  string
	Model   string       // e.g., "text-embedding-3-small"
	Dims    int          // output dimensions (default: 1536)
	Client  *http.Client // optional HTTP client
}

// NewAPIProvider creates an API-based embedding provider.
func NewAPIProvider(cfg APIConfig) *APIProvider {
	if cfg.Dims <= 0 {
		cfg.Dims = 1536
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &APIProvider{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		dims:    cfg.Dims,
		client:  client,
	}
}

// Embed generates an embedding vector via the API.
func (p *APIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"input": text,
		"model": p.model,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return result.Data[0].Embedding, nil
}

// Dimensions returns the vector dimensionality.
func (p *APIProvider) Dimensions() int {
	return p.dims
}

// DetectProvider creates an embedding provider.
// API-based embedding (OpenAI) is only used when explicitly enabled via
// YCODE_EMBEDDING_API=true, to prevent unexpected API costs. The OPENAI_API_KEY
// environment variable must also be set.
// Returns a SimpleHashProvider as the default — fast, free, local-only.
func DetectProvider() Provider {
	if os.Getenv("YCODE_EMBEDDING_API") == "true" {
		if key := os.Getenv("OPENAI_API_KEY"); key != "" {
			return NewAPIProvider(APIConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  key,
				Model:   "text-embedding-3-small",
				Dims:    1536,
			})
		}
	}

	// Default: hash-based provider — no API calls, no cost.
	return NewSimpleHashProvider(384)
}
