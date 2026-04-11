package vfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MaxFileSize is the maximum file size for read/write operations (10 MB).
const MaxFileSize = 10 * 1024 * 1024

// SymlinkPrompter asks the user to approve a symlink whose resolved target
// falls outside all allowed directories.
type SymlinkPrompter func(ctx context.Context, link, target string) (bool, error)

// VFS enforces filesystem access boundaries. All paths are validated against
// a set of allowed directories before any I/O operation proceeds.
type VFS struct {
	mu          sync.RWMutex
	allowedDirs []string // normalized absolute paths with trailing separator
	prompter    SymlinkPrompter
}

// New creates a VFS with the given allowed directories.
// Each directory is resolved to an absolute path and normalized with a
// trailing path separator to prevent prefix-matching attacks
// (e.g. /tmp/foo must not match /tmp/foobar).
func New(allowedDirs []string, prompter SymlinkPrompter) (*VFS, error) {
	normalized := make([]string, 0, len(allowedDirs))
	seen := make(map[string]bool)
	for _, d := range allowedDirs {
		abs, err := filepath.Abs(filepath.Clean(d))
		if err != nil {
			return nil, err
		}
		// Resolve symlinks on the allowed dir itself so that on systems
		// where paths like /var → /private/var (macOS), the resolved
		// form is used for boundary checks.
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if !strings.HasSuffix(abs, string(os.PathSeparator)) {
			abs += string(os.PathSeparator)
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		normalized = append(normalized, abs)
	}
	return &VFS{
		allowedDirs: normalized,
		prompter:    prompter,
	}, nil
}

// SetSymlinkPrompter sets the callback used to ask the user for approval
// when a symlink resolves outside all allowed directories.
func (v *VFS) SetSymlinkPrompter(p SymlinkPrompter) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.prompter = p
}

// AllowedDirs returns the list of allowed directories (without trailing separators)
// suitable for display to users.
func (v *VFS) AllowedDirs() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	dirs := make([]string, len(v.allowedDirs))
	for i, d := range v.allowedDirs {
		dirs[i] = strings.TrimSuffix(d, string(os.PathSeparator))
	}
	return dirs
}

// isWithinAllowed checks whether absPath falls under any allowed directory.
func (v *VFS) isWithinAllowed(absPath string) bool {
	// Exact match on the directory itself (without separator).
	for _, dir := range v.allowedDirs {
		trimmed := strings.TrimSuffix(dir, string(os.PathSeparator))
		if absPath == trimmed {
			return true
		}
		if strings.HasPrefix(absPath, dir) {
			return true
		}
	}
	return false
}

// getPrompter returns the current symlink prompter under read lock.
func (v *VFS) getPrompter() SymlinkPrompter {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.prompter
}
