package memory

import (
	"fmt"
	"path/filepath"
	"sort"
)

// Manager coordinates memory operations across global and project scopes.
type Manager struct {
	projectStore *Store
	globalStore  *Store // may be nil if no global dir configured
	index        *Index // project-level index

	bleveSearcher  *BleveSearcher  // optional Bleve full-text search
	vectorSearcher *VectorSearcher // optional vector semantic search
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

// Save persists a memory to the appropriate store based on scope.
func (m *Manager) Save(mem *Memory) error {
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

	return nil
}

// Recall retrieves memories matching a query from all scopes.
// Project-scoped memories are ranked higher than global ones.
func (m *Manager) Recall(query string, maxResults int) ([]SearchResult, error) {
	memories, err := m.All()
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	// Build a name→memory lookup for enriching search results.
	memByName := make(map[string]*Memory, len(memories))
	for _, mem := range memories {
		memByName[mem.Name] = mem
	}

	// Collect results from all available search backends.
	var results []SearchResult
	seen := make(map[string]bool)

	// Vector search (semantic similarity).
	if m.vectorSearcher != nil {
		vectorResults := m.vectorSearcher.Search(query, maxResults)
		for _, r := range vectorResults {
			if !seen[r.Memory.Name] {
				seen[r.Memory.Name] = true
				if full, ok := memByName[r.Memory.Name]; ok {
					r.Memory = full
				}
				results = append(results, r)
			}
		}
	}

	// Bleve search (full-text keyword matching).
	if m.bleveSearcher != nil {
		bleveResults := m.bleveSearcher.Search(query, maxResults*2)
		for _, r := range bleveResults {
			if !seen[r.Memory.Name] {
				seen[r.Memory.Name] = true
				if full, ok := memByName[r.Memory.Name]; ok {
					r.Memory = full
				}
				results = append(results, r)
			}
		}
	}

	// Fall back to keyword matching if no search backends are available.
	if m.vectorSearcher == nil && m.bleveSearcher == nil {
		results = Search(memories, query, maxResults*2)
	}

	// Apply decay scoring and scope boost.
	for i := range results {
		results[i].Score = DecayScore(results[i].Score, results[i].Memory)
		// Project-scoped memories get a slight boost.
		if results[i].Memory.EffectiveScope() == ScopeProject {
			results[i].Score *= 1.1
		}
	}

	// Re-sort after scope boost.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Trim to requested count.
	if len(results) > maxResults {
		results = results[:maxResults]
	}

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
