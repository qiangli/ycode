// Package vector provides a chromem-go backed vector store for semantic similarity search.
//
// chromem-go is a pure Go embeddable vector database with zero external dependencies.
// It supports cosine similarity, Euclidean distance, and dot product metrics,
// with optional persistence to disk via GZIP-compressed GOB files.
package vector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/philippgille/chromem-go"

	"github.com/qiangli/ycode/internal/storage"
)

// Store implements storage.VectorStore backed by chromem-go.
type Store struct {
	mu            sync.RWMutex
	db            *chromem.DB
	dir           string
	embeddingFunc chromem.EmbeddingFunc
}

// Option configures a Store.
type Option func(*Store)

// WithEmbeddingFunc sets a custom embedding function for the vector store.
// If not set, documents must include pre-computed embeddings.
func WithEmbeddingFunc(fn storage.EmbeddingFunc) Option {
	return func(s *Store) {
		s.embeddingFunc = func(ctx context.Context, text string) ([]float32, error) {
			return fn(ctx, text)
		}
	}
}

// Open creates or opens a persistent vector store at the given directory.
func Open(dir string, opts ...Option) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create vector dir: %w", err)
	}

	dbPath := filepath.Join(dir, "vectors")
	db, err := chromem.NewPersistentDB(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("open chromem db: %w", err)
	}

	s := &Store{
		db:  db,
		dir: dir,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// getOrCreateCollection returns an existing collection or creates a new one.
func (s *Store) getOrCreateCollection(name string) (*chromem.Collection, error) {
	col := s.db.GetCollection(name, s.embeddingFunc)
	if col != nil {
		return col, nil
	}

	col, err := s.db.GetOrCreateCollection(name, nil, s.embeddingFunc)
	if err != nil {
		return nil, fmt.Errorf("create collection %q: %w", name, err)
	}
	return col, nil
}

// AddDocuments adds documents to a collection.
func (s *Store) AddDocuments(ctx context.Context, collection string, docs []storage.VectorDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	col, err := s.getOrCreateCollection(collection)
	if err != nil {
		return err
	}

	chromDocs := make([]chromem.Document, len(docs))
	for i, doc := range docs {
		metadata := make(map[string]string)
		for k, v := range doc.Metadata {
			metadata[k] = v
		}
		chromDocs[i] = chromem.Document{
			ID:        doc.ID,
			Content:   doc.Content,
			Metadata:  metadata,
			Embedding: doc.Embedding,
		}
	}

	return col.AddDocuments(ctx, chromDocs, runtime(ctx))
}

// Query finds the most similar documents to the query embedding.
func (s *Store) Query(ctx context.Context, collection string, queryEmbedding []float32, maxResults int) ([]storage.SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	col := s.db.GetCollection(collection, s.embeddingFunc)
	if col == nil {
		return nil, nil
	}

	n := min(maxResults, col.Count())
	if n == 0 {
		return nil, nil
	}

	results, err := col.QueryEmbedding(ctx, queryEmbedding, n, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("query collection %q: %w", collection, err)
	}

	return toSearchResults(results), nil
}

// QueryByText finds similar documents using a text query.
func (s *Store) QueryByText(ctx context.Context, collection string, query string, maxResults int) ([]storage.SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	col := s.db.GetCollection(collection, s.embeddingFunc)
	if col == nil {
		return nil, nil
	}

	n := min(maxResults, col.Count())
	if n == 0 {
		return nil, nil
	}

	results, err := col.Query(ctx, query, n, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("query by text %q: %w", collection, err)
	}

	return toSearchResults(results), nil
}

// DeleteDocument removes a document by ID from a collection.
func (s *Store) DeleteDocument(_ context.Context, collection string, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	col := s.db.GetCollection(collection, s.embeddingFunc)
	if col == nil {
		return nil
	}

	return col.Delete(context.Background(), nil, nil, docID)
}

// DeleteCollection removes an entire collection.
func (s *Store) DeleteCollection(_ context.Context, collection string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.DeleteCollection(collection)
}

// Collections returns all collection names.
func (s *Store) Collections(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cols := s.db.ListCollections()
	names := make([]string, 0, len(cols))
	for name := range cols {
		names = append(names, name)
	}
	return names, nil
}

// Close is a no-op for chromem-go (data is persisted on write).
func (s *Store) Close() error {
	return nil
}

func toSearchResults(results []chromem.Result) []storage.SearchResult {
	out := make([]storage.SearchResult, len(results))
	for i, r := range results {
		metadata := make(map[string]string)
		for k, v := range r.Metadata {
			metadata[k] = v
		}
		out[i] = storage.SearchResult{
			Document: storage.Document{
				ID:       r.ID,
				Content:  r.Content,
				Metadata: metadata,
			},
			Score: float64(r.Similarity),
		}
	}
	return out
}

// runtime returns the concurrency level from context, defaulting to 1.
func runtime(_ context.Context) int {
	return 1
}

// compile-time interface check.
var _ storage.VectorStore = (*Store)(nil)
