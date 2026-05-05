package memory

import (
	"os"
	"path/filepath"
)

// MemoryFileNames are the filenames to discover.
var MemoryFileNames = []string{
	"MEMORY.md",
	"CLAUDE.md",
	"CLAUDE.local.md",
}

// DiscoverMemoryFiles walks from startDir to root, looking for MEMORY.md files.
func DiscoverMemoryFiles(startDir string) []string {
	var paths []string
	dir := startDir

	for {
		for _, name := range MemoryFileNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return paths
}
