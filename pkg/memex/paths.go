package memex

import (
	"fmt"
	"path"
	"strings"

	"github.com/qiangli/ycode/pkg/memex/memory"
)

// Path-scheme constants. Memory entries live under /memory/, memo notes
// under /memos/. Examples:
//
//	/memory/global/feedback/prefer-table-output.md
//	/memory/project/<id>/notes/auth-flow.md
//	/memos/42
//	/memos/tag/auth/
const (
	prefixMemory = "/memory"
	prefixMemos  = "/memos"
)

// MemoryPath builds the canonical virtual path for a memory entry.
func MemoryPath(m memory.Memory) string {
	scope := m.EffectiveScope()
	scopePath := strings.TrimPrefix(m.ScopePath, "/")

	parts := []string{prefixMemory, string(scope)}
	if scopePath != "" {
		parts = append(parts, scopePath)
	}
	parts = append(parts, string(m.Type), m.Name+".md")
	return path.Join(parts...)
}

// ParseMemoryPath decodes a virtual path back into a memory's scope/type/name.
// Accepts paths previously produced by MemoryPath. The scopePath segment is
// any directory components between the scope and the type.
func ParseMemoryPath(p string) (scope memory.Scope, scopePath string, mType memory.Type, name string, err error) {
	clean := strings.TrimPrefix(path.Clean(p), prefixMemory+"/")
	if clean == p {
		return "", "", "", "", fmt.Errorf("not a memory path: %q", p)
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 3 {
		return "", "", "", "", fmt.Errorf("memory path too short: %q", p)
	}
	scope = memory.Scope(parts[0])
	mType = memory.Type(parts[len(parts)-2])
	leaf := parts[len(parts)-1]
	name = strings.TrimSuffix(leaf, ".md")
	if mid := parts[1 : len(parts)-2]; len(mid) > 0 {
		scopePath = path.Join(mid...)
	}
	return scope, scopePath, mType, name, nil
}

// MemoPath builds the canonical virtual path for a memo by id.
func MemoPath(id string) string {
	return path.Join(prefixMemos, id)
}

// MemoTagPath builds the virtual directory path for memos with a given tag.
func MemoTagPath(tag string) string {
	return path.Join(prefixMemos, "tag", tag) + "/"
}

// ParseMemoPath extracts the memo id from a virtual path. Returns empty
// string if the path is not a flat /memos/<id> path.
func ParseMemoPath(p string) (id string, ok bool) {
	clean := strings.TrimPrefix(path.Clean(p), prefixMemos+"/")
	if clean == p || strings.Contains(clean, "/") {
		return "", false
	}
	return clean, true
}
