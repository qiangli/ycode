package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest describes a plugin's metadata and capabilities.
type Manifest struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Description string           `json:"description,omitempty"`
	Author      string           `json:"author,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Hooks       []HookDefinition `json:"hooks,omitempty"`
}

// ToolDefinition describes a tool provided by a plugin.
type ToolDefinition struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	InputSchema     json.RawMessage `json:"input_schema"`
	Command         string          `json:"command"`                    // shell command to execute
	AlwaysAvailable bool            `json:"always_available,omitempty"` // sent in every request vs deferred
}

// HookDefinition describes a lifecycle hook provided by a plugin.
type HookDefinition struct {
	Event   string `json:"event"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // milliseconds
}

// LoadManifest reads and parses a plugin.json file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	if m.Name == "" {
		return nil, fmt.Errorf("manifest %s: name is required", path)
	}
	if m.Version == "" {
		m.Version = "0.0.0"
	}

	return &m, nil
}

// DiscoverManifests scans directories for plugin.json files.
// It searches each dir for immediate subdirectories containing a plugin.json.
func DiscoverManifests(dirs ...string) ([]DiscoveredPlugin, error) {
	var plugins []DiscoveredPlugin

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scan plugin dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(dir, entry.Name(), "plugin.json")
			manifest, err := LoadManifest(manifestPath)
			if err != nil {
				continue // skip invalid plugins
			}
			plugins = append(plugins, DiscoveredPlugin{
				Dir:      filepath.Join(dir, entry.Name()),
				Manifest: manifest,
			})
		}
	}

	return plugins, nil
}

// DiscoveredPlugin pairs a manifest with its filesystem location.
type DiscoveredPlugin struct {
	Dir      string
	Manifest *Manifest
}
