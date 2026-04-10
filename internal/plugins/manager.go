package plugins

import (
	"fmt"
	"sync"
)

// Kind identifies where a plugin came from.
type Kind string

const (
	KindBuiltin  Kind = "builtin"
	KindBundled  Kind = "bundled"
	KindExternal Kind = "external"
)

// Manifest describes a plugin.
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Kind        Kind   `json:"kind"`
	Enabled     bool   `json:"enabled"`
	EntryPoint  string `json:"entry_point,omitempty"`
}

// Manager handles plugin lifecycle.
type Manager struct {
	mu      sync.RWMutex
	plugins map[string]*Manifest
}

// NewManager creates a new plugin manager.
func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]*Manifest),
	}
}

// Install adds a plugin.
func (m *Manager) Install(manifest *Manifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.plugins[manifest.Name]; exists {
		return fmt.Errorf("plugin %q already installed", manifest.Name)
	}
	m.plugins[manifest.Name] = manifest
	return nil
}

// Uninstall removes a plugin.
func (m *Manager) Uninstall(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.plugins[name]; !exists {
		return fmt.Errorf("plugin %q not found", name)
	}
	delete(m.plugins, name)
	return nil
}

// Enable enables a plugin.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.Enabled = true
	return nil
}

// Disable disables a plugin.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.Enabled = false
	return nil
}

// List returns all installed plugins.
func (m *Manager) List() []*Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Manifest, 0, len(m.plugins))
	for _, p := range m.plugins {
		result = append(result, p)
	}
	return result
}

// Get returns a plugin by name.
func (m *Manager) Get(name string) (*Manifest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[name]
	return p, ok
}

// EnabledPlugins returns only enabled plugins.
func (m *Manager) EnabledPlugins() []*Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Manifest
	for _, p := range m.plugins {
		if p.Enabled {
			result = append(result, p)
		}
	}
	return result
}
