package memory

import (
	"context"
	"log/slog"

	"github.com/qiangli/ycode/internal/storage"
)

const vectorCollection = "memory"

// VectorSearcher provides vector-based semantic search for memories.
// It complements BleveSearcher (keyword) with embedding similarity.
type VectorSearcher struct {
	store storage.VectorStore
}

// NewVectorSearcher creates a vector-based memory searcher.
func NewVectorSearcher(store storage.VectorStore) *VectorSearcher {
	return &VectorSearcher{store: store}
}

// IndexMemory adds a memory's embedding to the vector store.
func (v *VectorSearcher) IndexMemory(mem *Memory, embedding []float32) {
	ctx := context.Background()
	doc := storage.VectorDocument{
		Document: storage.Document{
			ID:      mem.Name,
			Content: mem.Name + " " + mem.Description + " " + mem.Content,
			Metadata: map[string]string{
				"name":        mem.Name,
				"description": mem.Description,
				"type":        string(mem.Type),
				"scope":       string(mem.EffectiveScope()),
			},
		},
		Embedding: embedding,
	}
	if err := v.store.AddDocuments(ctx, vectorCollection, []storage.VectorDocument{doc}); err != nil {
		slog.Debug("vector: index memory", "name", mem.Name, "error", err)
	}
}

// RemoveMemory removes a memory from the vector store.
func (v *VectorSearcher) RemoveMemory(name string) {
	ctx := context.Background()
	if err := v.store.DeleteDocument(ctx, vectorCollection, name); err != nil {
		slog.Debug("vector: remove memory", "name", name, "error", err)
	}
}

// Search finds memories by semantic similarity using a text query.
// The vector store handles embedding the query text internally.
func (v *VectorSearcher) Search(query string, maxResults int) []SearchResult {
	ctx := context.Background()
	results, err := v.store.QueryByText(ctx, vectorCollection, query, maxResults)
	if err != nil {
		slog.Debug("vector: search memories", "error", err)
		return nil
	}

	var out []SearchResult
	for _, r := range results {
		out = append(out, SearchResult{
			Memory: &Memory{
				Name:        r.Document.Metadata["name"],
				Description: r.Document.Metadata["description"],
				Type:        Type(r.Document.Metadata["type"]),
				Scope:       Scope(r.Document.Metadata["scope"]),
				Content:     r.Document.Content,
			},
			Score: r.Score,
		})
	}
	return out
}

// SearchByEmbedding finds memories using a pre-computed embedding vector.
func (v *VectorSearcher) SearchByEmbedding(embedding []float32, maxResults int) []SearchResult {
	ctx := context.Background()
	results, err := v.store.Query(ctx, vectorCollection, embedding, maxResults)
	if err != nil {
		slog.Debug("vector: search by embedding", "error", err)
		return nil
	}

	var out []SearchResult
	for _, r := range results {
		out = append(out, SearchResult{
			Memory: &Memory{
				Name:        r.Document.Metadata["name"],
				Description: r.Document.Metadata["description"],
				Type:        Type(r.Document.Metadata["type"]),
				Scope:       Scope(r.Document.Metadata["scope"]),
				Content:     r.Document.Content,
			},
			Score: r.Score,
		})
	}
	return out
}
