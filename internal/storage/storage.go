// Package storage is a backwards-compatibility shim that re-exports the public
// surface of github.com/qiangli/ycode/pkg/memex/store. It exists so callers can
// migrate to the pkg path incrementally; new code should import the pkg path
// directly.
//
// Deprecated: use github.com/qiangli/ycode/pkg/memex/store. This shim will be
// removed in Phase 6 of the memex refactor.
package storage

import (
	"context"

	store "github.com/qiangli/ycode/pkg/memex/store"
)

// Backend interfaces.
type (
	KVStore     = store.KVStore
	SQLStore    = store.SQLStore
	VectorStore = store.VectorStore
	SearchIndex = store.SearchIndex
	Result      = store.Result
	Row         = store.Row
	Rows        = store.Rows
)

// Document and search types.
type (
	Document       = store.Document
	VectorDocument = store.VectorDocument
	SearchResult   = store.SearchResult
	EmbeddingFunc  = store.EmbeddingFunc
)

// Phase enums.
type Phase = store.Phase

const (
	Phase1 = store.Phase1
	Phase2 = store.Phase2
	Phase3 = store.Phase3
)

// Status types.
type (
	Status        = store.Status
	BackendStatus = store.BackendStatus
)

// Manager + Config.
type (
	Manager = store.Manager
	Config  = store.Config
)

// NewManager creates a storage manager (Phase 1 init).
func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
	return store.NewManager(ctx, cfg)
}

// DefaultEvictionInterval re-exported for callers that reference it.
const DefaultEvictionInterval = store.DefaultEvictionInterval
