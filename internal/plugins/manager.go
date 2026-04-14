package plugins

import (
	"fmt"
	"sync"
)

// ManagedPlugin wraps a Manifest with runtime state for the Manager.
type ManagedPlugin struct {
	Manifest *Manifest
	Dir      string // filesystem location
	Enabled  bool
}

// Manager handles plugin lifecycle.
type Manager struct {
	mu      sync.RWMutex
	plugins map[string]*ManagedPlugin
}

// NewManager creates a new plugin manager.
func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]*ManagedPlugin),
	}
}

// Install adds a plugin from a discovered manifest.
func (m *Manager) Install(dp DiscoveredPlugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.plugins[dp.Manifest.Name]; exists {
		return fmt.Errorf("plugin %q already installed", dp.Manifest.Name)
	}
	m.plugins[dp.Manifest.Name] = &ManagedPlugin{
		Manifest: dp.Manifest,
		Dir:      dp.Dir,
		Enabled:  true,
	}
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
func (m *Manager) List() []*ManagedPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*ManagedPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		result = append(result, p)
	}
	return result
}

// Get returns a plugin by name.
func (m *Manager) Get(name string) (*ManagedPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[name]
	return p, ok
}

// EnabledPlugins returns only enabled plugins.
func (m *Manager) EnabledPlugins() []*ManagedPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*ManagedPlugin
	for _, p := range m.plugins {
		if p.Enabled {
			result = append(result, p)
		}
	}
	return result
}
