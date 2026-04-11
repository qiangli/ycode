// Package storage defines interfaces for ycode's persistence layer.
//
// The storage system uses four complementary backends:
//   - KVStore: fast key-value lookups (bbolt) for config, permissions, metadata
//   - SQLStore: structured queries (SQLite) for sessions, messages, tasks
//   - VectorStore: similarity search (chromem-go) for semantic code/memory search
//   - SearchIndex: full-text search (Bleve) for keyword search across all content
//
// All backends are pure Go with permissive licenses and zero CGO dependencies.
package storage

import (
	"context"
	"io"
	"time"
)

// KVStore provides fast key-value storage with bucket-based namespacing.
// Backed by bbolt (go.etcd.io/bbolt).
type KVStore interface {
	// Get retrieves a value by bucket and key. Returns nil if not found.
	Get(bucket, key string) ([]byte, error)

	// Put stores a value in a bucket. Creates the bucket if it doesn't exist.
	Put(bucket, key string, value []byte) error

	// Delete removes a key from a bucket. No error if key doesn't exist.
	Delete(bucket, key string) error

	// List returns all keys in a bucket.
	List(bucket string) ([]string, error)

	// ForEach iterates over all key-value pairs in a bucket.
	ForEach(bucket string, fn func(key string, value []byte) error) error

	io.Closer
}

// SQLStore provides structured data storage with SQL queries.
// Backed by modernc.org/sqlite.
type SQLStore interface {
	// Exec executes a statement that doesn't return rows.
	Exec(ctx context.Context, query string, args ...any) (Result, error)

	// QueryRow executes a query that returns at most one row.
	QueryRow(ctx context.Context, query string, args ...any) Row

	// Query executes a query that returns rows.
	Query(ctx context.Context, query string, args ...any) (Rows, error)

	// Tx runs a function within a transaction.
	Tx(ctx context.Context, fn func(tx SQLStore) error) error

	// Migrate runs pending migrations.
	Migrate(ctx context.Context) error

	io.Closer
}

// Result is the result of an Exec call.
type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

// Row is a single row returned by QueryRow.
type Row interface {
	Scan(dest ...any) error
}

// Rows is an iterator over query results.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	io.Closer
}

// Document is a unit of content for vector and full-text indexing.
type Document struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// VectorDocument extends Document with an embedding vector.
type VectorDocument struct {
	Document
	Embedding []float32 `json:"embedding,omitempty"`
}

// SearchResult is a scored match from vector or full-text search.
type SearchResult struct {
	Document Document
	Score    float64
}

// EmbeddingFunc generates an embedding vector for the given text.
// This is called by the VectorStore when adding documents without pre-computed embeddings.
type EmbeddingFunc func(ctx context.Context, text string) ([]float32, error)

// VectorStore provides semantic similarity search over document embeddings.
// Backed by chromem-go (github.com/philippgille/chromem-go).
type VectorStore interface {
	// AddDocuments adds documents to a collection. If embeddings are nil,
	// the store's configured EmbeddingFunc is used to generate them.
	AddDocuments(ctx context.Context, collection string, docs []VectorDocument) error

	// Query finds the most similar documents to the query embedding.
	Query(ctx context.Context, collection string, queryEmbedding []float32, maxResults int) ([]SearchResult, error)

	// QueryByText finds similar documents using a text query (embedding is computed internally).
	QueryByText(ctx context.Context, collection string, query string, maxResults int) ([]SearchResult, error)

	// DeleteDocument removes a document by ID from a collection.
	DeleteDocument(ctx context.Context, collection string, docID string) error

	// DeleteCollection removes an entire collection and its data.
	DeleteCollection(ctx context.Context, collection string) error

	// Collections returns the names of all collections.
	Collections(ctx context.Context) ([]string, error)

	io.Closer
}

// SearchIndex provides full-text search with relevance scoring.
// Backed by Bleve (github.com/blevesearch/bleve/v2).
type SearchIndex interface {
	// Index adds or updates a document in a named index.
	Index(ctx context.Context, indexName string, doc Document) error

	// BatchIndex adds multiple documents to a named index.
	BatchIndex(ctx context.Context, indexName string, docs []Document) error

	// Search performs a full-text search query against a named index.
	Search(ctx context.Context, indexName string, query string, maxResults int) ([]SearchResult, error)

	// Delete removes a document by ID from a named index.
	Delete(ctx context.Context, indexName string, docID string) error

	// DeleteIndex removes an entire index and its data.
	DeleteIndex(ctx context.Context, indexName string) error

	io.Closer
}

// Phase represents the initialization phase of a storage backend.
type Phase int

const (
	// Phase1 is instant startup: file config + KV store.
	Phase1 Phase = iota + 1
	// Phase2 is background init: SQLite database.
	Phase2
	// Phase3 is lazy init: search index + vector store.
	Phase3
)

// Status reports the state of each storage backend.
type Status struct {
	KV     BackendStatus `json:"kv"`
	SQL    BackendStatus `json:"sql"`
	Vector BackendStatus `json:"vector"`
	Search BackendStatus `json:"search"`
}

// BackendStatus is the state of a single storage backend.
type BackendStatus struct {
	Phase Phase     `json:"phase"`
	Ready bool      `json:"ready"`
	Error string    `json:"error,omitempty"`
	Since time.Time `json:"since,omitempty"`
}
