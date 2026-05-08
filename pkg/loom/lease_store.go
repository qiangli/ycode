package loom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// LeaseStore persists active leases. Implementations must be safe for
// concurrent use.
type LeaseStore interface {
	Get(id string) (Lease, bool)
	List() []Lease
	Put(lease Lease) error
	Delete(id string) error
}

// MemoryStore is an in-memory LeaseStore. Suitable for tests and for
// services that don't need crash recovery.
type MemoryStore struct {
	mu     sync.Mutex
	leases map[string]Lease
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{leases: make(map[string]Lease)}
}

func (s *MemoryStore) Get(id string) (Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.leases[id]
	return l, ok
}

func (s *MemoryStore) List() []Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Lease, 0, len(s.leases))
	for _, l := range s.leases {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *MemoryStore) Put(l Lease) error {
	if l.ID == "" {
		return fmt.Errorf("%w: empty lease ID", ErrInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leases[l.ID] = l
	return nil
}

func (s *MemoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, id)
	return nil
}

// FileStore wraps a MemoryStore with atomic JSON persistence. Every
// Put / Delete rewrites the file via temp + rename, mirroring the
// pattern in internal/gitserver/projects/projects.go.
type FileStore struct {
	path string
	mem  *MemoryStore
	mu   sync.Mutex
}

// NewFileStore loads (or creates) a JSON-backed LeaseStore at path.
// Missing file is treated as empty store; corrupt file is an error.
func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty file store path", ErrInvalidRequest)
	}
	fs := &FileStore{
		path: path,
		mem:  NewMemoryStore(),
	}
	if err := fs.load(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (s *FileStore) Get(id string) (Lease, bool) { return s.mem.Get(id) }
func (s *FileStore) List() []Lease               { return s.mem.List() }

func (s *FileStore) Put(l Lease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.mem.Put(l); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.mem.Delete(id); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *FileStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loom: read lease store %s: %w", s.path, err)
	}
	var list []Lease
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("loom: decode lease store %s: %w", s.path, err)
	}
	for _, l := range list {
		_ = s.mem.Put(l)
	}
	return nil
}

func (s *FileStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	list := s.mem.List()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
