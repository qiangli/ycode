package memory

import (
	"fmt"
	"path/filepath"
)

// Manager coordinates memory operations.
type Manager struct {
	store *Store
	index *Index
}

// NewManager creates a new memory manager.
func NewManager(dir string) (*Manager, error) {
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		store: store,
		index: NewIndex(dir),
	}, nil
}

// Save persists a memory and updates the index.
func (m *Manager) Save(mem *Memory) error {
	if err := m.store.Save(mem); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	filename := filepath.Base(mem.FilePath)
	if err := m.index.AddEntry(mem.Name, filename, mem.Description); err != nil {
		return fmt.Errorf("update index: %w", err)
	}

	return nil
}

// Recall retrieves memories matching a query.
func (m *Manager) Recall(query string, maxResults int) ([]SearchResult, error) {
	memories, err := m.store.List()
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	results := Search(memories, query, maxResults)

	// Apply decay scoring.
	for i := range results {
		results[i].Score = DecayScore(results[i].Score, results[i].Memory)
	}

	return results, nil
}

// Forget removes a memory by name.
func (m *Manager) Forget(name string) error {
	memories, err := m.store.List()
	if err != nil {
		return err
	}

	for _, mem := range memories {
		if mem.Name == name {
			filename := filepath.Base(mem.FilePath)
			if err := m.store.Delete(mem.FilePath); err != nil {
				return fmt.Errorf("delete memory: %w", err)
			}
			return m.index.RemoveEntry(filename)
		}
	}

	return fmt.Errorf("memory %q not found", name)
}

// All returns all stored memories.
func (m *Manager) All() ([]*Memory, error) {
	return m.store.List()
}

// ReadIndex returns the MEMORY.md contents.
func (m *Manager) ReadIndex() (string, error) {
	return m.index.Read()
}

// Store returns the underlying store.
func (m *Manager) Store() *Store {
	return m.store
}
