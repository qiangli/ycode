package memex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/memos"
)

// Node is a single entry in the VFS tree.
type Node struct {
	Path    string            // virtual path, e.g. /memory/global/feedback/x.md
	IsDir   bool              // true for virtual directories like /memory/global/
	Source  string            // "memory" or "memos"
	Size    int64             // 0 for directories
	ModTime time.Time         // last modification time
	Meta    map[string]string // type, scope, tags, visibility, etc.
}

// VFS presents memory entries and memo notes under a single virtual path
// tree, suitable for a wiki-style browser. The tree is computed on demand
// over the underlying stores; nothing is cached on disk beyond what those
// stores already persist.
type VFS interface {
	List(ctx context.Context, dirPath string) ([]Node, error)
	Stat(ctx context.Context, path string) (Node, error)
	Read(ctx context.Context, path string) ([]byte, Node, error)
	Write(ctx context.Context, path string, body []byte, meta map[string]string) error
	Delete(ctx context.Context, path string) error
}

// NewVFS returns the default VFS implementation backed by the given memory
// and memos stores. Either may be nil; the corresponding paths will be
// reported as empty / missing.
func NewVFS(mem *memory.Manager, m memos.Store) VFS {
	return &defaultVFS{mem: mem, memos: m}
}

type defaultVFS struct {
	mem   *memory.Manager
	memos memos.Store
}

func (v *defaultVFS) List(ctx context.Context, dirPath string) ([]Node, error) {
	dirPath = "/" + strings.Trim(dirPath, "/")
	switch {
	case dirPath == "/":
		out := []Node{}
		if v.mem != nil {
			out = append(out, Node{Path: prefixMemory, IsDir: true, Source: "memory"})
		}
		if v.memos != nil {
			out = append(out, Node{Path: prefixMemos, IsDir: true, Source: "memos"})
		}
		return out, nil
	case strings.HasPrefix(dirPath, prefixMemory):
		return v.listMemory(dirPath)
	case strings.HasPrefix(dirPath, prefixMemos):
		return v.listMemos(ctx, dirPath)
	}
	return nil, fmt.Errorf("vfs: unknown root in %q", dirPath)
}

func (v *defaultVFS) listMemory(dirPath string) ([]Node, error) {
	if v.mem == nil {
		return nil, nil
	}
	all, err := v.mem.All()
	if err != nil {
		return nil, err
	}
	seen := map[string]Node{}
	prefix := strings.TrimSuffix(dirPath, "/") + "/"
	for _, m := range all {
		full := MemoryPath(*m)
		if !strings.HasPrefix(full, prefix) && full != dirPath {
			continue
		}
		rel := strings.TrimPrefix(full, prefix)
		if rel == "" {
			continue
		}
		head := rel
		isDir := false
		if i := strings.Index(rel, "/"); i >= 0 {
			head = rel[:i]
			isDir = true
		}
		key := prefix + head
		if isDir {
			seen[key] = Node{Path: key, IsDir: true, Source: "memory"}
		} else {
			seen[key] = Node{
				Path:    key,
				Source:  "memory",
				Size:    int64(len(m.Content)),
				ModTime: m.UpdatedAt,
				Meta: map[string]string{
					"type":  string(m.Type),
					"scope": string(m.EffectiveScope()),
				},
			}
		}
	}
	out := make([]Node, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	return out, nil
}

func (v *defaultVFS) listMemos(ctx context.Context, dirPath string) ([]Node, error) {
	if v.memos == nil {
		return nil, nil
	}
	if dirPath == prefixMemos {
		res, err := v.memos.List(ctx, memos.ListOptions{PageSize: 100})
		if err != nil {
			return nil, err
		}
		out := make([]Node, 0, len(res.Memos))
		for _, m := range res.Memos {
			out = append(out, Node{
				Path:    MemoPath(m.ID),
				Source:  "memos",
				Size:    int64(len(m.Content)),
				ModTime: m.UpdatedAt,
				Meta: map[string]string{
					"visibility": m.Visibility,
					"state":      m.State,
					"title":      m.Property.Title,
				},
			})
		}
		return out, nil
	}
	if strings.HasPrefix(dirPath, prefixMemos+"/tag/") {
		tag := strings.TrimPrefix(dirPath, prefixMemos+"/tag/")
		tag = strings.TrimSuffix(tag, "/")
		ms, err := v.memos.SearchByTag(ctx, tag, 100)
		if err != nil {
			return nil, err
		}
		out := make([]Node, 0, len(ms))
		for _, m := range ms {
			out = append(out, Node{
				Path:    MemoPath(m.ID),
				Source:  "memos",
				Size:    int64(len(m.Content)),
				ModTime: m.UpdatedAt,
				Meta:    map[string]string{"title": m.Property.Title},
			})
		}
		return out, nil
	}
	return nil, nil
}

func (v *defaultVFS) Stat(ctx context.Context, p string) (Node, error) {
	body, n, err := v.Read(ctx, p)
	if err != nil {
		return Node{}, err
	}
	if n.Size == 0 {
		n.Size = int64(len(body))
	}
	return n, nil
}

func (v *defaultVFS) Read(ctx context.Context, p string) ([]byte, Node, error) {
	switch {
	case strings.HasPrefix(p, prefixMemory+"/"):
		if v.mem == nil {
			return nil, Node{}, fmt.Errorf("memory not configured")
		}
		_, _, _, name, err := ParseMemoryPath(p)
		if err != nil {
			return nil, Node{}, err
		}
		m, err := v.findMemoryByName(name)
		if err != nil {
			return nil, Node{}, err
		}
		return []byte(m.Content), Node{
			Path: p, Source: "memory",
			Size: int64(len(m.Content)), ModTime: m.UpdatedAt,
			Meta: map[string]string{"type": string(m.Type), "scope": string(m.EffectiveScope())},
		}, nil
	case strings.HasPrefix(p, prefixMemos+"/"):
		if v.memos == nil {
			return nil, Node{}, fmt.Errorf("memos not configured")
		}
		id, ok := ParseMemoPath(p)
		if !ok {
			return nil, Node{}, fmt.Errorf("vfs: not a memo path: %q", p)
		}
		m, err := v.memos.Get(ctx, id)
		if err != nil {
			return nil, Node{}, err
		}
		return []byte(m.Content), Node{
			Path: p, Source: "memos",
			Size: int64(len(m.Content)), ModTime: m.UpdatedAt,
			Meta: map[string]string{"visibility": m.Visibility, "state": m.State, "title": m.Property.Title},
		}, nil
	}
	return nil, Node{}, fmt.Errorf("vfs: unknown path: %q", p)
}

func (v *defaultVFS) Write(ctx context.Context, p string, body []byte, meta map[string]string) error {
	switch {
	case strings.HasPrefix(p, prefixMemory+"/"):
		if v.mem == nil {
			return fmt.Errorf("memory not configured")
		}
		scope, scopePath, mType, name, err := ParseMemoryPath(p)
		if err != nil {
			return err
		}
		m := &memory.Memory{
			Name:      name,
			Scope:     scope,
			ScopePath: scopePath,
			Type:      mType,
			Content:   string(body),
		}
		if d, ok := meta["description"]; ok {
			m.Description = d
		}
		return v.mem.Save(m)
	case strings.HasPrefix(p, prefixMemos+"/"):
		if v.memos == nil {
			return fmt.Errorf("memos not configured")
		}
		id, ok := ParseMemoPath(p)
		if !ok {
			return fmt.Errorf("vfs: not a memo path: %q", p)
		}
		if id == "" {
			return v.memos.Create(ctx, &memos.Memo{Content: string(body)})
		}
		_, err := v.memos.Update(ctx, id, string(body))
		return err
	}
	return fmt.Errorf("vfs: unknown path: %q", p)
}

// findMemoryByName scans all memories for one with the given Name. The
// memory subsystem stores files by name, so this is O(n) but bounded by the
// modest number of persistent memories (hundreds, not millions).
func (v *defaultVFS) findMemoryByName(name string) (*memory.Memory, error) {
	all, err := v.mem.All()
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.Name == name {
			return m, nil
		}
	}
	return nil, fmt.Errorf("memory %q not found", name)
}

func (v *defaultVFS) Delete(ctx context.Context, p string) error {
	switch {
	case strings.HasPrefix(p, prefixMemory+"/"):
		if v.mem == nil {
			return fmt.Errorf("memory not configured")
		}
		_, _, _, name, err := ParseMemoryPath(p)
		if err != nil {
			return err
		}
		return v.mem.Forget(name)
	case strings.HasPrefix(p, prefixMemos+"/"):
		if v.memos == nil {
			return fmt.Errorf("memos not configured")
		}
		id, ok := ParseMemoPath(p)
		if !ok {
			return fmt.Errorf("vfs: not a memo path: %q", p)
		}
		return v.memos.Delete(ctx, id)
	}
	return fmt.Errorf("vfs: unknown path: %q", p)
}
