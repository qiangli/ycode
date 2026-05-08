package selfinit

import (
	"errors"
	"os"
	"os/exec"
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

// DetectYcodeCommand returns (command, args) suitable for a stdio MCP
// entry. Detection priority:
//
//  1. ycode on PATH                                  → "ycode"
//  2. ~/go/bin/ycode exists                          → absolute path
//  3. ~/.local/bin/ycode exists                      → absolute path
//  4. fallback                                       → "go run github.com/qiangli/ycode/cmd/ycode@latest"
//
// originalCmd / originalArgs are the values from the manifest (used as
// the "ycode is on PATH" branch when applicable).
func DetectYcodeCommand(originalCmd string, originalArgs []string) (string, []string) {
	if _, err := exec.LookPath("ycode"); err == nil {
		if originalCmd != "" {
			return originalCmd, append([]string(nil), originalArgs...)
		}
		return "ycode", []string{"mcp", "serve"}
	}
	home, _ := os.UserHomeDir()
	for _, cand := range []string{
		filepath.Join(home, "go", "bin", "ycode"),
		filepath.Join(home, ".local", "bin", "ycode"),
	} {
		if isExecutable(cand) {
			return cand, []string{"mcp", "serve"}
		}
	}
	// Final fallback: invoke via Go's module cache. First call is slow
	// (network + compile); subsequent calls reuse the cached binary.
	return "go", []string{"run", "github.com/qiangli/ycode/cmd/ycode@latest", "mcp", "serve"}
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// ErrNoGitRoot indicates SelfInit was invoked outside any git repo.
// Callers may treat this as a no-op rather than an error.
var ErrNoGitRoot = errors.New("selfinit: not in a git repository")
