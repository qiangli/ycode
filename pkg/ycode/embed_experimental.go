//go:build experimental

package ycode

import (
	"context"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/embedding"
)

// Embed returns a vector embedding for text using the Agent's configured
// embedding provider. When WithEmbeddingProvider was not used, the Agent
// lazily detects one on first call via embedding.DetectProvider() — the same
// env-precedence ladder (YCODE_EMBEDDING_API → YCODE_OLLAMA_EMBEDDING →
// TF-IDF fallback) the internal store uses.
func (a *Agent) Embed(ctx context.Context, text string) ([]float32, error) {
	p, err := a.ensureEmbedder()
	if err != nil {
		return nil, err
	}
	return p.Embed(ctx, text)
}

// EmbedBatch returns embeddings for a slice of texts. The current
// implementation calls the underlying Provider sequentially; the API
// contract is batched so a future provider that supports native batching
// can drop in without changing callers.
func (a *Agent) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	p, err := a.ensureEmbedder()
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := p.Embed(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("embed[%d]: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// EmbeddingDimensions returns the vector dimensionality of the configured
// embedding provider. Callers should rely on this rather than hard-coding a
// dimension when allocating storage.
func (a *Agent) EmbeddingDimensions() (int, error) {
	p, err := a.ensureEmbedder()
	if err != nil {
		return 0, err
	}
	return p.Dimensions(), nil
}

func (a *Agent) ensureEmbedder() (embedding.Provider, error) {
	a.embedOnce.Do(func() {
		if a.embedProvider == nil {
			a.embedProvider = embedding.DetectProvider()
		}
	})
	if a.embedProvider == nil {
		return nil, fmt.Errorf("embed: no embedding provider configured")
	}
	return a.embedProvider, nil
}
