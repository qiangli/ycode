package memory

import (
	"context"
	"log/slog"

	"github.com/qiangli/ycode/internal/storage"
)

const bleveIndexName = "memory"

// BleveSearcher provides Bleve-backed full-text search for memories.
// When set on the Manager, it replaces the simple keyword matching in Search().
type BleveSearcher struct {
	index storage.SearchIndex
}

// NewBleveSearcher creates a Bleve-backed memory searcher.
func NewBleveSearcher(index storage.SearchIndex) *BleveSearcher {
	return &BleveSearcher{index: index}
}

// IndexMemory adds or updates a memory in the Bleve index.
func (b *BleveSearcher) IndexMemory(mem *Memory) {
	ctx := context.Background()
	doc := storage.Document{
		ID:      mem.Name,
		Content: mem.Name + " " + mem.Description + " " + mem.Content,
		Metadata: map[string]string{
			"name":        mem.Name,
			"description": mem.Description,
			"type":        string(mem.Type),
			"scope":       string(mem.EffectiveScope()),
		},
	}
	if err := b.index.Index(ctx, bleveIndexName, doc); err != nil {
		slog.Debug("bleve: index memory", "name", mem.Name, "error", err)
	}
}

// RemoveMemory removes a memory from the Bleve index.
func (b *BleveSearcher) RemoveMemory(name string) {
	ctx := context.Background()
	if err := b.index.Delete(ctx, bleveIndexName, name); err != nil {
		slog.Debug("bleve: remove memory", "name", name, "error", err)
	}
}

// Search performs a full-text search for memories matching the query.
func (b *BleveSearcher) Search(query string, maxResults int) []SearchResult {
	ctx := context.Background()
	results, err := b.index.Search(ctx, bleveIndexName, query, maxResults)
	if err != nil {
		slog.Debug("bleve: search memories", "error", err)
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

// IndexAll indexes all provided memories into Bleve.
func (b *BleveSearcher) IndexAll(memories []*Memory) {
	ctx := context.Background()
	var docs []storage.Document
	for _, mem := range memories {
		docs = append(docs, storage.Document{
			ID:      mem.Name,
			Content: mem.Name + " " + mem.Description + " " + mem.Content,
			Metadata: map[string]string{
				"name":        mem.Name,
				"description": mem.Description,
				"type":        string(mem.Type),
				"scope":       string(mem.EffectiveScope()),
			},
		})
	}
	if len(docs) > 0 {
		if err := b.index.BatchIndex(ctx, bleveIndexName, docs); err != nil {
			slog.Debug("bleve: batch index memories", "error", err)
		}
	}
}
