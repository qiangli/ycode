package ycode

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/embedding"
)

func TestEmbedWithProvider(t *testing.T) {
	stubEmb := embedding.NewSimpleHashProvider(16)
	a, err := NewAgent(
		WithProvider(newStubProvider(api.ProviderOpenAI)),
		WithoutBuiltinTools(),
		WithEmbeddingProvider(stubEmb),
	)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	vec, err := a.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 16 {
		t.Errorf("vector dim: want 16, got %d", len(vec))
	}

	dim, err := a.EmbeddingDimensions()
	if err != nil {
		t.Fatalf("EmbeddingDimensions: %v", err)
	}
	if dim != 16 {
		t.Errorf("Dimensions: want 16, got %d", dim)
	}

	// Determinism: same input → same vector under SimpleHashProvider.
	vec2, _ := a.Embed(context.Background(), "hello world")
	if len(vec) != len(vec2) {
		t.Fatalf("re-embed length mismatch")
	}
	for i := range vec {
		if vec[i] != vec2[i] {
			t.Errorf("non-deterministic embedding at index %d: %f vs %f", i, vec[i], vec2[i])
			break
		}
	}
}

func TestEmbedBatch(t *testing.T) {
	a, err := NewAgent(
		WithProvider(newStubProvider(api.ProviderOpenAI)),
		WithoutBuiltinTools(),
		WithEmbeddingProvider(embedding.NewSimpleHashProvider(8)),
	)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	vecs, err := a.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 8 {
			t.Errorf("vec %d dim: want 8, got %d", i, len(v))
		}
	}
}

func TestEmbedLazyDetectFallback(t *testing.T) {
	// No WithEmbeddingProvider: should lazy-init via embedding.DetectProvider()
	// which falls through to TF-IDF when no env vars are set. We do not assert
	// dimension exactly (TF-IDF default is 384) but expect non-zero.
	a, err := NewAgent(
		WithProvider(newStubProvider(api.ProviderOpenAI)),
		WithoutBuiltinTools(),
	)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	dim, err := a.EmbeddingDimensions()
	if err != nil {
		t.Fatalf("EmbeddingDimensions: %v", err)
	}
	if dim <= 0 {
		t.Errorf("expected non-zero dim from lazy detect, got %d", dim)
	}
}
