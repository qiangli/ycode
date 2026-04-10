package scratchpad

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager handles scratch file operations.
type Manager struct {
	dir string // e.g., .ycode/scratchpad/
}

// NewManager creates a new scratchpad manager.
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create scratchpad dir: %w", err)
	}
	return &Manager{dir: dir}, nil
}

// Create writes a new scratch file.
func (m *Manager) Create(name, content string) error {
	path := filepath.Join(m.dir, sanitizeName(name)+".md")
	return os.WriteFile(path, []byte(content), 0o644)
}

// Read returns the content of a scratch file.
func (m *Manager) Read(name string) (string, error) {
	path := filepath.Join(m.dir, sanitizeName(name)+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Update overwrites a scratch file.
func (m *Manager) Update(name, content string) error {
	return m.Create(name, content)
}

// Delete removes a scratch file.
func (m *Manager) Delete(name string) error {
	path := filepath.Join(m.dir, sanitizeName(name)+".md")
	return os.Remove(path)
}

// List returns all scratch file names.
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names, nil
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return -1
	}, name)
	if name == "" {
		name = "scratch"
	}
	return name
}
