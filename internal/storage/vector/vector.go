// Package vector is a backwards-compatibility shim that re-exports
// github.com/qiangli/ycode/pkg/memex/store/vector.
//
// Deprecated: use github.com/qiangli/ycode/pkg/memex/store/vector. This shim
// will be removed in Phase 6 of the memex refactor.
package vector

import (
	pkgvector "github.com/qiangli/ycode/pkg/memex/store/vector"
)

type (
	Store  = pkgvector.Store
	Option = pkgvector.Option
)

// Open creates or opens a chromem-go vector store at the given directory.
func Open(dir string, opts ...Option) (*Store, error) { return pkgvector.Open(dir, opts...) }

// WithEmbeddingFunc sets a custom embedding function.
var WithEmbeddingFunc = pkgvector.WithEmbeddingFunc

// WithConcurrency sets the number of goroutines used for embedding computation.
var WithConcurrency = pkgvector.WithConcurrency

// WithCompression enables GZIP compression for persisted vector data.
var WithCompression = pkgvector.WithCompression
