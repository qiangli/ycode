package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/fileops"
)

const (
	// MaxIndexLines is the maximum number of lines in MEMORY.md.
	MaxIndexLines = 200
)

// Index manages the MEMORY.md file.
type Index struct {
	path string
}

// NewIndex creates a new index manager.
func NewIndex(dir string) *Index {
	return &Index{path: filepath.Join(dir, "MEMORY.md")}
}

// AddEntry adds a pointer to a memory file in MEMORY.md.
func (idx *Index) AddEntry(title, filename, description string) error {
	existing, _ := os.ReadFile(idx.path)

	// Check for existing entry.
	entry := fmt.Sprintf("- [%s](%s) — %s", title, filename, description)
	if strings.Contains(string(existing), filename) {
		// Update existing entry.
		lines := strings.Split(string(existing), "\n")
		var updated []string
		for _, line := range lines {
			if strings.Contains(line, filename) {
				updated = append(updated, entry)
			} else {
				updated = append(updated, line)
			}
		}
		return fileops.AtomicWriteFile(idx.path, []byte(strings.Join(updated, "\n")), 0o644)
	}

	// Append new entry.
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entry + "\n"

	// Enforce line limit.
	lines := strings.Split(content, "\n")
	if len(lines) > MaxIndexLines {
		lines = lines[:MaxIndexLines]
		content = strings.Join(lines, "\n")
	}

	return fileops.AtomicWriteFile(idx.path, []byte(content), 0o644)
}

// RemoveEntry removes an entry from MEMORY.md.
func (idx *Index) RemoveEntry(filename string) error {
	data, err := os.ReadFile(idx.path)
	if err != nil {
		return nil // nothing to remove
	}

	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, line := range lines {
		if !strings.Contains(line, filename) {
			kept = append(kept, line)
		}
	}

	return fileops.AtomicWriteFile(idx.path, []byte(strings.Join(kept, "\n")), 0o644)
}

// Read returns the content of MEMORY.md.
func (idx *Index) Read() (string, error) {
	data, err := os.ReadFile(idx.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}
