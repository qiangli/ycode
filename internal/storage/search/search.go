// Package search provides a Bleve-backed full-text search index.
//
// Bleve is a pure Go full-text search library with BM25 scoring,
// fuzzy matching, phrase queries, faceted search, and multi-language
// text analyzers. It indexes documents with metadata for filtering
// and returns relevance-scored results.
package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/qiangli/ycode/internal/storage"
)

// Store implements storage.SearchIndex backed by Bleve.
type Store struct {
	mu      sync.RWMutex
	dir     string
	indexes map[string]bleve.Index
}

// Open creates a new search store at the given directory.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create search dir: %w", err)
	}
	return &Store{
		dir:     dir,
		indexes: make(map[string]bleve.Index),
	}, nil
}

// getOrCreateIndex lazily opens or creates a Bleve index.
// Returns an error if the store has been closed.
func (s *Store) getOrCreateIndex(name string) (bleve.Index, error) {
	s.mu.RLock()
	if s.indexes == nil {
		s.mu.RUnlock()
		return nil, fmt.Errorf("search store is closed")
	}
	idx, ok := s.indexes[name]
	s.mu.RUnlock()
	if ok {
		return idx, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store has been closed — indexes map set to nil during shutdown.
	if s.indexes == nil {
		return nil, fmt.Errorf("search store is closed")
	}

	// Double-check after acquiring write lock.
	if idx, ok := s.indexes[name]; ok {
		return idx, nil
	}

	indexPath := filepath.Join(s.dir, name+".bleve")

	// Try to open existing index.
	idx, err := bleve.Open(indexPath)
	if err == nil {
		s.indexes[name] = idx
		return idx, nil
	}

	// Create new index with default mapping.
	idx, err = bleve.New(indexPath, buildMapping())
	if err != nil {
		return nil, fmt.Errorf("create bleve index %q: %w", name, err)
	}

	s.indexes[name] = idx
	return idx, nil
}

// buildMapping creates the default document mapping for ycode content.
func buildMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()

	// Document mapping for code/memory/session content.
	docMapping := bleve.NewDocumentMapping()

	// Content field: full-text analyzed.
	contentField := bleve.NewTextFieldMapping()
	contentField.Analyzer = "standard"
	contentField.Store = true
	contentField.IncludeTermVectors = true
	docMapping.AddFieldMappingsAt("content", contentField)

	// ID field: keyword (exact match).
	idField := bleve.NewKeywordFieldMapping()
	docMapping.AddFieldMappingsAt("id", idField)

	// Metadata fields: keyword for filtering.
	metaField := bleve.NewKeywordFieldMapping()
	docMapping.AddFieldMappingsAt("path", metaField)
	docMapping.AddFieldMappingsAt("type", metaField)
	docMapping.AddFieldMappingsAt("language", metaField)
	docMapping.AddFieldMappingsAt("scope", metaField)
	docMapping.AddFieldMappingsAt("role", metaField)

	im.DefaultMapping = docMapping
	return im
}

// Index adds or updates a document in a named index.
func (s *Store) Index(_ context.Context, indexName string, doc storage.Document) error {
	idx, err := s.getOrCreateIndex(indexName)
	if err != nil {
		return err
	}

	// Flatten document for Bleve indexing.
	bleveDoc := map[string]any{
		"id":      doc.ID,
		"content": doc.Content,
	}
	for k, v := range doc.Metadata {
		bleveDoc[k] = v
	}

	return idx.Index(doc.ID, bleveDoc)
}

// BatchIndex adds multiple documents to a named index.
func (s *Store) BatchIndex(_ context.Context, indexName string, docs []storage.Document) error {
	idx, err := s.getOrCreateIndex(indexName)
	if err != nil {
		return err
	}

	batch := idx.NewBatch()
	for _, doc := range docs {
		bleveDoc := map[string]any{
			"id":      doc.ID,
			"content": doc.Content,
		}
		for k, v := range doc.Metadata {
			bleveDoc[k] = v
		}
		if err := batch.Index(doc.ID, bleveDoc); err != nil {
			return fmt.Errorf("batch index doc %q: %w", doc.ID, err)
		}
	}

	return idx.Batch(batch)
}

// Search performs a full-text search against a named index.
func (s *Store) Search(_ context.Context, indexName string, query string, maxResults int) ([]storage.SearchResult, error) {
	idx, err := s.getOrCreateIndex(indexName)
	if err != nil {
		return nil, err
	}

	searchReq := bleve.NewSearchRequest(bleve.NewQueryStringQuery(query))
	searchReq.Size = maxResults
	searchReq.Fields = []string{"*"}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", indexName, err)
	}

	var results []storage.SearchResult
	for _, hit := range result.Hits {
		doc := storage.Document{
			ID:       hit.ID,
			Metadata: make(map[string]string),
		}
		if content, ok := hit.Fields["content"].(string); ok {
			doc.Content = content
		}
		for k, v := range hit.Fields {
			if k == "content" || k == "id" {
				continue
			}
			if sv, ok := v.(string); ok {
				doc.Metadata[k] = sv
			}
		}
		results = append(results, storage.SearchResult{
			Document: doc,
			Score:    hit.Score,
		})
	}

	return results, nil
}

// Delete removes a document by ID from a named index.
func (s *Store) Delete(_ context.Context, indexName string, docID string) error {
	idx, err := s.getOrCreateIndex(indexName)
	if err != nil {
		return err
	}
	return idx.Delete(docID)
}

// DeleteIndex removes an entire index and its data.
func (s *Store) DeleteIndex(_ context.Context, indexName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if idx, ok := s.indexes[indexName]; ok {
		idx.Close()
		delete(s.indexes, indexName)
	}

	indexPath := filepath.Join(s.dir, indexName+".bleve")
	return os.RemoveAll(indexPath)
}

// Compact closes and reopens all open indexes, triggering Bleve's internal
// segment merging to reclaim disk space and optimize query performance.
func (s *Store) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for name, idx := range s.indexes {
		// Close triggers internal segment merge on shutdown.
		if err := idx.Close(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("compact index %q: close: %w", name, err)
			}
			delete(s.indexes, name)
			continue
		}

		// Reopen the index.
		indexPath := filepath.Join(s.dir, name+".bleve")
		reopened, err := bleve.Open(indexPath)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("compact index %q: reopen: %w", name, err)
			}
			delete(s.indexes, name)
			continue
		}
		s.indexes[name] = reopened
	}
	return firstErr
}

// Close closes all open indexes.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for name, idx := range s.indexes {
		if err := idx.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close index %q: %w", name, err)
		}
	}
	s.indexes = nil
	return firstErr
}

// compile-time interface check.
var _ storage.SearchIndex = (*Store)(nil)
