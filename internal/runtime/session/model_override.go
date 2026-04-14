package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// SessionMetadata holds per-session metadata that persists alongside the JSONL.
type SessionMetadata struct {
	ModelOverride string `json:"model_override,omitempty"`
	// Future: add more per-session settings here.
}

// MetadataStore manages per-session metadata files.
type MetadataStore struct {
	mu sync.RWMutex
	// Cache of loaded metadata keyed by session dir.
	cache map[string]*SessionMetadata
}

// NewMetadataStore creates a new metadata store.
func NewMetadataStore() *MetadataStore {
	return &MetadataStore{
		cache: make(map[string]*SessionMetadata),
	}
}

const metadataFilename = "session_meta.json"

// Get returns the metadata for a session, loading from disk if not cached.
func (ms *MetadataStore) Get(sessionDir string) (*SessionMetadata, error) {
	ms.mu.RLock()
	if m, ok := ms.cache[sessionDir]; ok {
		ms.mu.RUnlock()
		return m, nil
	}
	ms.mu.RUnlock()

	// Load from disk.
	m, err := ms.loadFromDisk(sessionDir)
	if err != nil {
		return nil, err
	}

	ms.mu.Lock()
	ms.cache[sessionDir] = m
	ms.mu.Unlock()
	return m, nil
}

// SetModelOverride sets the model override for a session and persists it.
func (ms *MetadataStore) SetModelOverride(sessionDir string, model string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	m, ok := ms.cache[sessionDir]
	if !ok {
		m = &SessionMetadata{}
		ms.cache[sessionDir] = m
	}
	m.ModelOverride = model
	return ms.saveToDisk(sessionDir, m)
}

// ClearModelOverride removes the model override for a session.
func (ms *MetadataStore) ClearModelOverride(sessionDir string) error {
	return ms.SetModelOverride(sessionDir, "")
}

// ModelOverride returns the current model override for a session, or empty.
func (ms *MetadataStore) ModelOverride(sessionDir string) string {
	m, err := ms.Get(sessionDir)
	if err != nil {
		return ""
	}
	return m.ModelOverride
}

func (ms *MetadataStore) loadFromDisk(sessionDir string) (*SessionMetadata, error) {
	path := filepath.Join(sessionDir, metadataFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionMetadata{}, nil
		}
		return nil, err
	}
	var m SessionMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return &SessionMetadata{}, nil // treat corrupt file as empty
	}
	return &m, nil
}

func (ms *MetadataStore) saveToDisk(sessionDir string, m *SessionMetadata) error {
	path := filepath.Join(sessionDir, metadataFilename)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
