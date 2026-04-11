package prompt

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// MaxFileContentBudget per instruction file.
	MaxFileContentBudget = 4000
	// MaxTotalBudget for all instruction files combined.
	MaxTotalBudget = 12000
)

// InstructionFileNames are the filenames to search for.
var InstructionFileNames = []string{
	"CLAUDE.md",
	"CLAUDE.local.md",
	".ycode/CLAUDE.md",
	".ycode/instructions.md",
}

// DiscoverInstructionFiles walks from startDir to root, collecting instruction files.
// Files are deduplicated by content hash.
func DiscoverInstructionFiles(startDir string) []ContextFile {
	seen := make(map[string]bool) // content hash -> seen
	var files []ContextFile
	totalChars := 0

	dir := startDir
	for {
		for _, name := range InstructionFileNames {
			path := filepath.Join(dir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			// Skip empty files.
			if len(content) == 0 {
				continue
			}

			// Dedup by content hash.
			hash := fmt.Sprintf("%x", sha256.Sum256(content))
			if seen[hash] {
				continue
			}
			seen[hash] = true

			// Resolve #import directives before budget enforcement.
			text := string(content)
			visited := map[string]bool{path: true}
			text = ResolveImports(text, filepath.Dir(path), visited, 0)

			// Truncate to budget.
			if len(text) > MaxFileContentBudget {
				text = text[:MaxFileContentBudget] + "\n... (truncated)"
			}

			// Check total budget.
			if totalChars+len(text) > MaxTotalBudget {
				break
			}
			totalChars += len(text)

			files = append(files, ContextFile{
				Path:    path,
				Content: text,
				Hash:    hash,
			})
		}

		// Move to parent directory.
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}

	return files
}
