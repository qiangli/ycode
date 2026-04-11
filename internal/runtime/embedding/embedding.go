// Package embedding provides pluggable text embedding for vector search.
//
// The embedding system supports multiple providers:
//   - LLM API providers (Anthropic, OpenAI, etc.) via their embedding endpoints
//   - Local models (future: ONNX runtime or similar)
//   - No-op provider for testing
//
// The embedding function is injected into the vector store so that documents
// can be added with automatic embedding generation.
package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
)

// Provider generates embedding vectors from text.
type Provider interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dimensions returns the vector dimensionality for this provider.
	Dimensions() int
}

// SimpleHashProvider is a deterministic embedding provider that uses hashing.
// It generates consistent embeddings without requiring an external API.
// Useful for testing and as a fallback when no API-based provider is available.
//
// The embeddings are not semantically meaningful but produce consistent
// results for identical inputs and somewhat similar results for similar inputs.
type SimpleHashProvider struct {
	dims int
}

// NewSimpleHashProvider creates a hash-based embedding provider.
func NewSimpleHashProvider(dims int) *SimpleHashProvider {
	if dims <= 0 {
		dims = 384
	}
	return &SimpleHashProvider{dims: dims}
}

// Embed generates a deterministic embedding from text using hashing.
func (p *SimpleHashProvider) Embed(_ context.Context, text string) ([]float32, error) {
	// Normalize text.
	text = strings.ToLower(strings.TrimSpace(text))

	// Generate multiple hashes to fill the vector.
	vec := make([]float32, p.dims)
	for i := 0; i < p.dims; i++ {
		// Hash text + position to get deterministic values.
		data := append([]byte(text), byte(i), byte(i>>8))
		h := sha256.Sum256(data)
		bits := binary.LittleEndian.Uint32(h[:4])
		// Map to [-1, 1] range.
		vec[i] = float32(bits)/float32(math.MaxUint32)*2 - 1
	}

	// L2 normalize.
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec, nil
}

// Dimensions returns the vector dimensionality.
func (p *SimpleHashProvider) Dimensions() int {
	return p.dims
}
