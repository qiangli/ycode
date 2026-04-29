package memory

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Manager coordinates memory operations across global and project scopes.
type Manager struct {
	projectStore *Store
	globalStore  *Store // may be nil if no global dir configured
	index        *Index // project-level index

	bleveSearcher  *BleveSearcher  // optional Bleve full-text search
	vectorSearcher *VectorSearcher // optional vector semantic search
	entityIndex    *EntityIndex    // optional entity extraction and linking
	provider       MemoryProvider  // optional pluggable provider (supplements stores)
}

// NewManager creates a new memory manager with a project-scoped store.
func NewManager(dir string) (*Manager, error) {
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		projectStore: store,
		index:        NewIndex(dir),
	}, nil
}

// NewManagerWithGlobal creates a manager with both global and project stores.
// Global memories are shared across all projects; project memories are scoped.
func NewManagerWithGlobal(globalDir, projectDir string) (*Manager, error) {
	projectStore, err := NewStore(projectDir)
	if err != nil {
		return nil, fmt.Errorf("project store: %w", err)
	}

	var globalStore *Store
	if globalDir != "" {
		globalStore, err = NewStore(globalDir)
		if err != nil {
			return nil, fmt.Errorf("global store: %w", err)
		}
	}

	return &Manager{
		projectStore: projectStore,
		globalStore:  globalStore,
		index:        NewIndex(projectDir),
	}, nil
}

// SetBleveSearcher attaches a Bleve-backed searcher for full-text memory search.
func (m *Manager) SetBleveSearcher(b *BleveSearcher) {
	m.bleveSearcher = b
}

// SetVectorSearcher attaches a vector-based searcher for semantic memory search.
func (m *Manager) SetVectorSearcher(v *VectorSearcher) {
	m.vectorSearcher = v
}

// SetProvider attaches an optional MemoryProvider that supplements the
// default Store-based flow. When set, lifecycle hooks (e.g., OnMemoryWrite)
// are called after the primary store operations succeed.
func (m *Manager) SetProvider(p MemoryProvider) {
	m.provider = p
}

// Save persists a memory to the appropriate store based on scope.
func (m *Manager) Save(mem *Memory) error {
	ctx := context.Background()
	tracer := otel.Tracer("ycode.memory")
	_, span := tracer.Start(ctx, "ycode.memory.save",
		trace.WithAttributes(
			attribute.String("memory.name", mem.Name),
			attribute.String("memory.type", string(mem.Type)),
			attribute.String("memory.scope", string(mem.EffectiveScope())),
			attribute.Float64("memory.importance", mem.Importance),
		))
	defer span.End()

	store := m.storeForScope(mem.EffectiveScope())
	if err := store.Save(mem); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	filename := filepath.Base(mem.FilePath)
	if err := m.index.AddEntry(mem.Name, filename, mem.Description); err != nil {
		return fmt.Errorf("update index: %w", err)
	}

	// Index in Bleve for full-text search.
	if m.bleveSearcher != nil {
		m.bleveSearcher.IndexMemory(mem)
	}

	// Extract and link entities (Phase 4).
	if m.entityIndex != nil {
		entities := ExtractEntities(mem.Content)
		mem.Entities = make([]string, 0, len(entities))
		for _, e := range entities {
			m.entityIndex.Link(e, mem.Name)
			mem.Entities = append(mem.Entities, e.Name)
		}
	}

	// Notify the pluggable provider if one is attached.
	if m.provider != nil {
		if err := m.provider.OnMemoryWrite(context.Background(), mem); err != nil {
			slog.Warn("memory provider OnMemoryWrite failed", "error", err)
		}
	}

	return nil
}

// Recall retrieves memories matching a query from all scopes.
// Uses Reciprocal Rank Fusion (RRF) across all available search backends,
// applies composite scoring (recency + importance), then MMR diversity re-ranking.
func (m *Manager) Recall(query string, maxResults int) ([]SearchResult, error) {
	ctx := context.Background()
	tracer := otel.Tracer("ycode.memory")
	_, span := tracer.Start(ctx, "ycode.memory.recall",
		trace.WithAttributes(
			attribute.String("memory.query", query),
			attribute.Int("memory.max_results", maxResults),
		))
	defer span.End()

	memories, err := m.All()
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	// Build a name→memory lookup for enriching search results.
	memByName := make(map[string]*Memory, len(memories))
	for _, mem := range memories {
		memByName[mem.Name] = mem
	}

	// Collect results from all available backends in parallel.
	resultSets := make(map[string][]SearchResult)

	if m.vectorSearcher != nil {
		vectorResults := m.vectorSearcher.Search(query, maxResults*2)
		for i := range vectorResults {
			if full, ok := memByName[vectorResults[i].Memory.Name]; ok {
				vectorResults[i].Memory = full
			}
			vectorResults[i].Source = "vector"
		}
		if len(vectorResults) > 0 {
			resultSets["vector"] = vectorResults
		}
	}

	if m.bleveSearcher != nil {
		bleveResults := m.bleveSearcher.Search(query, maxResults*2)
		for i := range bleveResults {
			if full, ok := memByName[bleveResults[i].Memory.Name]; ok {
				bleveResults[i].Memory = full
			}
			bleveResults[i].Source = "bleve"
		}
		if len(bleveResults) > 0 {
			resultSets["bleve"] = bleveResults
		}
	}

	// Entity-based retrieval (Phase 4 integration point).
	if m.entityIndex != nil {
		entityResults := m.entityIndex.SearchMemories(query, memByName, maxResults*2)
		if len(entityResults) > 0 {
			resultSets["entity"] = entityResults
		}
	}

	// Always run keyword search as a signal (not just fallback).
	keywordResults := Search(memories, query, maxResults*2)
	for i := range keywordResults {
		keywordResults[i].Source = "keyword"
	}
	if len(keywordResults) > 0 {
		resultSets["keyword"] = keywordResults
	}

	// Fuse results with RRF.
	fusionWeights := DefaultFusionWeights()
	results := ReciprocalRankFusion(resultSets, fusionWeights.RRFk)

	// Apply composite scoring on top of fused scores.
	weights := DefaultWeights()
	for i := range results {
		results[i].Score = CompositeScore(
			results[i].Score,                   // fused RRF score
			results[i].Memory.UpdatedAt,        // recency
			results[i].Memory.EffectiveValue(), // dynamic value (Phase 2)
			weights,
		)
		// Project-scoped memories get a slight boost.
		if results[i].Memory.EffectiveScope() == ScopeProject {
			results[i].Score *= 1.1
		}
		// Memories past their validity window get a penalty (Phase 6).
		if results[i].Memory.ValidUntil != nil && time.Now().After(*results[i].Memory.ValidUntil) {
			results[i].Score *= 0.3
		}
	}

	// Re-sort after composite scoring.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// MMR diversity re-ranking.
	results = MMRRerank(results, fusionWeights.MMRLambda, maxResults)

	span.SetAttributes(
		attribute.Int("memory.results_count", len(results)),
	)
	slog.Info("memory.recall",
		"query", query,
		"max_results", maxResults,
		"results_count", len(results),
	)

	return results, nil
}

// Forget removes a memory by name from all scopes.
func (m *Manager) Forget(name string) error {
	memories, err := m.All()
	if err != nil {
		return err
	}

	for _, mem := range memories {
		if mem.Name == name {
			store := m.storeForScope(mem.EffectiveScope())
			filename := filepath.Base(mem.FilePath)
			if err := store.Delete(mem.FilePath); err != nil {
				return fmt.Errorf("delete memory: %w", err)
			}
			if m.bleveSearcher != nil {
				m.bleveSearcher.RemoveMemory(name)
			}
			if m.entityIndex != nil {
				m.entityIndex.Unlink(name)
			}
			return m.index.RemoveEntry(filename)
		}
	}

	return fmt.Errorf("memory %q not found", name)
}

// All returns all stored memories from all scopes.
func (m *Manager) All() ([]*Memory, error) {
	projectMems, err := m.projectStore.List()
	if err != nil {
		return nil, fmt.Errorf("list project memories: %w", err)
	}

	// Tag project memories with scope if missing.
	for _, mem := range projectMems {
		if mem.Scope == "" {
			mem.Scope = ScopeProject
		}
	}

	if m.globalStore == nil {
		return projectMems, nil
	}

	globalMems, err := m.globalStore.List()
	if err != nil {
		return nil, fmt.Errorf("list global memories: %w", err)
	}

	// Tag global memories.
	for _, mem := range globalMems {
		mem.Scope = ScopeGlobal
	}

	return append(globalMems, projectMems...), nil
}

// ReadIndex returns the MEMORY.md contents.
func (m *Manager) ReadIndex() (string, error) {
	return m.index.Read()
}

// Store returns the project store (for backward compatibility).
func (m *Manager) Store() *Store {
	return m.projectStore
}

// GlobalStore returns the global store (may be nil).
func (m *Manager) GlobalStore() *Store {
	return m.globalStore
}

// storeForScope returns the appropriate store for a given scope.
func (m *Manager) storeForScope(scope Scope) *Store {
	if scope == ScopeGlobal && m.globalStore != nil {
		return m.globalStore
	}
	return m.projectStore
}
