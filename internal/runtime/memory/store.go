package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store handles file-based memory persistence.
type Store struct {
	dir string // e.g., ~/.ycode/projects/{hash}/memory/
}

// NewStore creates a new memory store at the given directory.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save writes a memory to a file with frontmatter.
func (s *Store) Save(mem *Memory) error {
	if mem.FilePath == "" {
		mem.FilePath = filepath.Join(s.dir, sanitizeFilename(mem.Name)+".md")
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", mem.Name)
	fmt.Fprintf(&b, "description: %s\n", mem.Description)
	fmt.Fprintf(&b, "type: %s\n", mem.Type)
	if mem.Scope != "" {
		fmt.Fprintf(&b, "scope: %s\n", mem.Scope)
	}
	b.WriteString("---\n\n")
	b.WriteString(mem.Content)

	mem.UpdatedAt = time.Now()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = mem.UpdatedAt
	}

	return os.WriteFile(mem.FilePath, []byte(b.String()), 0o644)
}

// Load reads a memory from a file.
func (s *Store) Load(path string) (*Memory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	mem := parseFrontmatter(string(data))
	mem.FilePath = path

	info, err := os.Stat(path)
	if err == nil {
		mem.UpdatedAt = info.ModTime()
	}

	return mem, nil
}

// List returns all memories in the store.
func (s *Store) List() ([]*Memory, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var memories []*Memory
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "MEMORY.md" {
			continue
		}
		mem, err := s.Load(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		memories = append(memories, mem)
	}
	return memories, nil
}

// Delete removes a memory file.
func (s *Store) Delete(path string) error {
	return os.Remove(path)
}

// Dir returns the store directory.
func (s *Store) Dir() string {
	return s.dir
}

// parseFrontmatter extracts frontmatter and content from a markdown file.
func parseFrontmatter(data string) *Memory {
	mem := &Memory{}

	if !strings.HasPrefix(data, "---\n") {
		mem.Content = data
		return mem
	}

	endIdx := strings.Index(data[4:], "\n---\n")
	if endIdx == -1 {
		mem.Content = data
		return mem
	}

	frontmatter := data[4 : 4+endIdx]
	mem.Content = strings.TrimSpace(data[4+endIdx+5:])

	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			mem.Name = strings.TrimSpace(value)
		case "description":
			mem.Description = strings.TrimSpace(value)
		case "type":
			mem.Type = Type(strings.TrimSpace(value))
		case "scope":
			mem.Scope = Scope(strings.TrimSpace(value))
		}
	}

	return mem
}

// sanitizeFilename converts a name to a safe filename.
func sanitizeFilename(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return -1
	}, name)
	if len(name) > 50 {
		name = name[:50]
	}
	if name == "" {
		name = "memory"
	}
	return name
}
