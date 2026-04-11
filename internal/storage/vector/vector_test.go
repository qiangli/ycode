package vector

import (
	"context"
	"math"
	"testing"

	"github.com/qiangli/ycode/internal/storage"
)

// mockEmbedding returns a deterministic embedding based on the text content.
// This is for testing only -- real usage would call an LLM embedding API.
func mockEmbedding(_ context.Context, text string) ([]float32, error) {
	// Simple hash-based embedding for testing.
	vec := make([]float32, 8)
	for i, c := range text {
		vec[i%8] += float32(c) / 1000.0
	}
	// Normalize.
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

func TestStore(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir, WithEmbeddingFunc(mockEmbedding))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	t.Run("AddAndQueryByText", func(t *testing.T) {
		docs := []storage.VectorDocument{
			{Document: storage.Document{ID: "1", Content: "authentication login handler"}},
			{Document: storage.Document{ID: "2", Content: "database migration schema"}},
			{Document: storage.Document{ID: "3", Content: "authentication password reset"}},
		}

		if err := s.AddDocuments(ctx, "test", docs); err != nil {
			t.Fatalf("AddDocuments: %v", err)
		}

		results, err := s.QueryByText(ctx, "test", "auth login", 2)
		if err != nil {
			t.Fatalf("QueryByText: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("QueryByText returned no results")
		}
		// Verify we got results back with valid scores.
		for _, r := range results {
			if r.Score <= 0 {
				t.Errorf("result %q has non-positive score: %f", r.Document.ID, r.Score)
			}
		}
	})

	t.Run("QueryEmptyCollection", func(t *testing.T) {
		results, err := s.QueryByText(ctx, "nonexistent", "anything", 5)
		if err != nil {
			t.Fatalf("QueryByText empty: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("results from empty collection = %d, want 0", len(results))
		}
	})

	t.Run("DeleteDocument", func(t *testing.T) {
		if err := s.DeleteDocument(ctx, "test", "1"); err != nil {
			t.Fatalf("DeleteDocument: %v", err)
		}

		results, err := s.QueryByText(ctx, "test", "authentication login handler", 5)
		if err != nil {
			t.Fatalf("QueryByText after delete: %v", err)
		}
		for _, r := range results {
			if r.Document.ID == "1" {
				t.Error("deleted document still returned")
			}
		}
	})

	t.Run("Collections", func(t *testing.T) {
		names, err := s.Collections(ctx)
		if err != nil {
			t.Fatalf("Collections: %v", err)
		}
		if len(names) == 0 {
			t.Error("Collections returned empty list")
		}
	})

	t.Run("DeleteCollection", func(t *testing.T) {
		if err := s.DeleteCollection(ctx, "test"); err != nil {
			t.Fatalf("DeleteCollection: %v", err)
		}
		results, err := s.QueryByText(ctx, "test", "anything", 5)
		if err != nil {
			t.Fatalf("QueryByText after DeleteCollection: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("results after DeleteCollection = %d, want 0", len(results))
		}
	})
}

func TestQueryWithEmbedding(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir, WithEmbeddingFunc(mockEmbedding))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Add with pre-computed embeddings.
	emb, _ := mockEmbedding(ctx, "hello world")
	docs := []storage.VectorDocument{
		{
			Document:  storage.Document{ID: "pre-1", Content: "hello world"},
			Embedding: emb,
		},
	}

	if err := s.AddDocuments(ctx, "precomputed", docs); err != nil {
		t.Fatalf("AddDocuments: %v", err)
	}

	results, err := s.Query(ctx, "precomputed", emb, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Document.ID != "pre-1" {
		t.Errorf("result ID = %q, want %q", results[0].Document.ID, "pre-1")
	}
}
