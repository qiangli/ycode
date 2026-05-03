package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/fileops"
)

// Store handles file-based memory persistence.
type Store struct {
	dir string // e.g., ~/.agents/ycode/projects/{hash}/memory/
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

	// Auto-set ValidUntil from TTLMinutes if not explicitly set.
	if mem.TTLMinutes > 0 && mem.ValidUntil == nil {
		expiry := time.Now().Add(time.Duration(mem.TTLMinutes) * time.Minute)
		mem.ValidUntil = &expiry
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", mem.Name)
	fmt.Fprintf(&b, "description: %s\n", mem.Description)
	fmt.Fprintf(&b, "type: %s\n", mem.Type)
	if mem.Scope != "" {
		fmt.Fprintf(&b, "scope: %s\n", mem.Scope)
	}
	if len(mem.Tags) > 0 {
		fmt.Fprintf(&b, "tags: %s\n", strings.Join(mem.Tags, ","))
	}
	if mem.ValueScore != 0 {
		fmt.Fprintf(&b, "value_score: %.4f\n", mem.ValueScore)
	}
	if mem.AccessCount > 0 {
		fmt.Fprintf(&b, "access_count: %d\n", mem.AccessCount)
	}
	if len(mem.Entities) > 0 {
		fmt.Fprintf(&b, "entities: %s\n", strings.Join(mem.Entities, ","))
	}
	if mem.TTLMinutes > 0 {
		fmt.Fprintf(&b, "ttl_minutes: %d\n", mem.TTLMinutes)
	}
	if mem.ValidFrom != nil {
		fmt.Fprintf(&b, "valid_from: %s\n", mem.ValidFrom.Format(time.RFC3339))
	}
	if mem.ValidUntil != nil {
		fmt.Fprintf(&b, "valid_until: %s\n", mem.ValidUntil.Format(time.RFC3339))
	}
	if mem.SupersededBy != "" {
		fmt.Fprintf(&b, "superseded_by: %s\n", mem.SupersededBy)
	}
	b.WriteString("---\n\n")
	b.WriteString(mem.Content)

	mem.UpdatedAt = time.Now()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = mem.UpdatedAt
	}

	return fileops.AtomicWriteFile(mem.FilePath, []byte(b.String()), 0o644)
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
		case "tags":
			if v := strings.TrimSpace(value); v != "" {
				mem.Tags = strings.Split(v, ",")
			}
		case "value_score":
			if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				mem.ValueScore = v
			}
		case "access_count":
			if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				mem.AccessCount = v
			}
		case "entities":
			if v := strings.TrimSpace(value); v != "" {
				mem.Entities = strings.Split(v, ",")
			}
		case "ttl_minutes":
			if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				mem.TTLMinutes = v
			}
		case "valid_from":
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
				mem.ValidFrom = &t
			}
		case "valid_until":
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
				mem.ValidUntil = &t
			}
		case "superseded_by":
			mem.SupersededBy = strings.TrimSpace(value)
		}
	}

	return mem
}

// BatchOp represents a single operation in a batch.
type BatchOp struct {
	// Op is the operation type: "save" or "delete".
	Op     string
	Memory *Memory
}

// batchRollback tracks a completed operation for undo purposes.
type batchRollback struct {
	op     string // "save" means we need to delete, "delete" means we need to restore
	path   string
	backup []byte // saved content for delete rollback
}

// Batch executes multiple operations atomically (best-effort for files).
// On any failure, previously completed operations in this batch are rolled back.
// Inspired by LangGraph's store batch() for multi-operation atomicity.
func (s *Store) Batch(ops []BatchOp) error {
	var completed []batchRollback

	for i, op := range ops {
		switch op.Op {
		case "save":
			if err := s.Save(op.Memory); err != nil {
				rollbackBatchOps(completed)
				return fmt.Errorf("batch op %d (save %q): %w", i, op.Memory.Name, err)
			}
			completed = append(completed, batchRollback{op: "save", path: op.Memory.FilePath})

		case "delete":
			// Read content for rollback before deleting.
			var backup []byte
			if data, err := os.ReadFile(op.Memory.FilePath); err == nil {
				backup = data
			}
			if err := s.Delete(op.Memory.FilePath); err != nil {
				rollbackBatchOps(completed)
				return fmt.Errorf("batch op %d (delete %q): %w", i, op.Memory.Name, err)
			}
			completed = append(completed, batchRollback{op: "delete", path: op.Memory.FilePath, backup: backup})

		default:
			rollbackBatchOps(completed)
			return fmt.Errorf("batch op %d: unknown op %q", i, op.Op)
		}
	}
	return nil
}

func rollbackBatchOps(ops []batchRollback) {
	for i := len(ops) - 1; i >= 0; i-- {
		switch ops[i].op {
		case "save":
			_ = os.Remove(ops[i].path)
		case "delete":
			if ops[i].backup != nil {
				_ = os.WriteFile(ops[i].path, ops[i].backup, 0o644)
			}
		}
	}
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
