package fileops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ProtectedDirs lists directories that should be skipped during filesystem walks
// to avoid triggering OS-level permission dialogs (e.g., macOS TCC prompts).
// These directories are user-private media/document folders that an agent has
// no legitimate reason to scan.
var ProtectedDirs = initProtectedDirs()

func initProtectedDirs() map[string]bool {
	if runtime.GOOS != "darwin" {
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// macOS TCC-protected directories that trigger permission dialogs.
	protected := []string{
		"Music",
		"Pictures",
		"Movies",
		"Downloads",
		"Desktop",
		"Documents",
	}

	dirs := make(map[string]bool, len(protected))
	for _, dir := range protected {
		dirs[filepath.Join(home, dir)] = true
	}
	return dirs
}

// IsProtectedPath returns true if the given absolute path is within a
// platform-protected directory that should not be accessed during walks.
// On non-macOS platforms, always returns false.
func IsProtectedPath(absPath string) bool {
	if ProtectedDirs == nil {
		return false
	}
	for dir := range ProtectedDirs {
		if absPath == dir || strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
