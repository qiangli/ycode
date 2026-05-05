package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PersistentEntityStore provides persistent entity storage backed by a JSON file.
// It wraps the in-memory EntityIndex with file-based persistence.
type PersistentEntityStore struct {
	idx  *EntityIndex
	path string
	mu   sync.Mutex
}

// NewPersistentEntityStore creates a persistent entity store at the given directory.
func NewPersistentEntityStore(dir string) *PersistentEntityStore {
	return &PersistentEntityStore{
		idx:  NewEntityIndex(),
		path: filepath.Join(dir, "_entity_index.json"),
	}
}

// Load reads the entity index from disk into memory.
func (s *PersistentEntityStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no persisted state yet
		}
		return fmt.Errorf("load entity store: %w", err)
	}

	var entities map[string]*Entity
	if err := json.Unmarshal(data, &entities); err != nil {
		return fmt.Errorf("parse entity store: %w", err)
	}

	s.idx.entities = entities
	return nil
}

// Save persists the current entity index to disk.
func (s *PersistentEntityStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.idx.entities, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal entity store: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create entity dir: %w", err)
	}

	return os.WriteFile(s.path, data, 0o644)
}

// Upsert adds or updates an entity and links it to a memory.
func (s *PersistentEntityStore) Upsert(entity Entity, memoryName string) error {
	s.mu.Lock()
	s.idx.Link(entity, memoryName)
	s.mu.Unlock()
	return s.Save()
}

// FindByName returns an entity by name (case-insensitive).
func (s *PersistentEntityStore) FindByName(name string) *Entity {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(name)
	return s.idx.entities[key]
}

// FindMemories returns memory names linked to an entity.
func (s *PersistentEntityStore) FindMemories(entityName string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx.FindMemories(entityName)
}

// UnlinkMemory removes all entity associations for a given memory.
func (s *PersistentEntityStore) UnlinkMemory(memoryName string) error {
	s.mu.Lock()
	s.idx.Unlink(memoryName)
	s.mu.Unlock()
	return s.Save()
}

// Index returns the underlying EntityIndex for read-only operations.
func (s *PersistentEntityStore) Index() *EntityIndex {
	return s.idx
}

// EntityCount returns the number of unique entities.
func (s *PersistentEntityStore) EntityCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.idx.entities)
}

// AllEntities returns all entities in the store.
func (s *PersistentEntityStore) AllEntities() []*Entity {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*Entity, 0, len(s.idx.entities))
	for _, e := range s.idx.entities {
		result = append(result, e)
	}
	return result
}
