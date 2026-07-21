package selfinit

import (
	"errors"
	"os"
	"path/filepath"
)

// FindGitRoot walks up from start (or any of its parents) looking for
// a .git/ directory or file (handles worktrees). Returns the absolute
// path to the repo root, or "" if no git repo is found before reaching
// the filesystem root.
//
// Bounded by maxAscend to defend against pathological mounts.
func FindGitRoot(start string) string {
	const maxAscend = 64
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	cur := abs
	for i := 0; i < maxAscend; i++ {
		gitPath := filepath.Join(cur, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
	return ""
}

// ErrNoGitRoot indicates SelfInit was invoked outside any git repo.
// Callers may treat this as a no-op rather than an error.
var ErrNoGitRoot = errors.New("selfinit: not in a git repository")
