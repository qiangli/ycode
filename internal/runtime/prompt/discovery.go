package prompt

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// MaxFileContentBudget per instruction file.
	MaxFileContentBudget = 2500
	// MaxTotalBudget for all instruction files combined.
	MaxTotalBudget = 6000
)

// InstructionFileNames are the filenames to search for.
var InstructionFileNames = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"CLAUDE.local.md",
	".agents/ycode/CLAUDE.md",
	".agents/ycode/instructions.md",
}

// DiscoverInstructionFiles walks from startDir up to ceiling, collecting instruction files.
// ceiling is the project root (e.g., git worktree root); discovery stops there.
// If ceiling is empty, startDir is used (no upward walking beyond the start).
// Files are deduplicated by content hash.
func DiscoverInstructionFiles(startDir, ceiling string) []ContextFile {
	seen := make(map[string]bool) // content hash -> seen
	var files []ContextFile
	totalChars := 0

	if ceiling == "" {
		ceiling = startDir
	}
	absCeiling, err := filepath.Abs(ceiling)
	if err != nil {
		absCeiling = ceiling
	}

	dir := startDir
	for {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			break
		}

		// Stop if we've gone above the project root.
		rel, err := filepath.Rel(absCeiling, absDir)
		if err != nil || strings.HasPrefix(rel, "..") {
			break
		}

		for _, name := range InstructionFileNames {
			path := filepath.Join(absDir, name)
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
		parent := filepath.Dir(absDir)
		if parent == absDir {
			break // reached filesystem root
		}
		dir = parent
	}

	return files
}

// DiscoverGlobalInstructionFiles loads user-level instruction files from the
// ycode config directory (e.g., ~/.agents/ycode/). These apply to all projects.
func DiscoverGlobalInstructionFiles(configDir string) []ContextFile {
	if configDir == "" {
		return nil
	}

	globalNames := []string{
		"AGENTS.md",
		"CLAUDE.md",
	}

	var files []ContextFile
	for _, name := range globalNames {
		path := filepath.Join(configDir, name)
		content, err := os.ReadFile(path)
		if err != nil || len(content) == 0 {
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		text := string(content)
		if len(text) > MaxFileContentBudget {
			text = text[:MaxFileContentBudget] + "\n... (truncated)"
		}
		files = append(files, ContextFile{
			Path:    path,
			Content: text,
			Hash:    hash,
		})
	}
	return files
}

// LoadConfiguredInstructions resolves instruction paths from config.
// Supports absolute paths, relative paths (resolved from projectRoot),
// ~/home-relative paths, and http/https URLs.
func LoadConfiguredInstructions(paths []string, projectRoot string) []ContextFile {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var files []ContextFile

	for _, raw := range paths {
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			content := fetchURL(raw)
			if content == "" {
				continue
			}
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			if seen[hash] {
				continue
			}
			seen[hash] = true
			if len(content) > MaxFileContentBudget {
				content = content[:MaxFileContentBudget] + "\n... (truncated)"
			}
			files = append(files, ContextFile{
				Path:    raw,
				Content: content,
				Hash:    hash,
			})
			continue
		}

		// Resolve path.
		resolved := raw
		if strings.HasPrefix(raw, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				resolved = filepath.Join(home, raw[2:])
			}
		} else if !filepath.IsAbs(raw) {
			resolved = filepath.Join(projectRoot, raw)
		}

		content, err := os.ReadFile(resolved)
		if err != nil || len(content) == 0 {
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		if seen[hash] {
			continue
		}
		seen[hash] = true
		text := string(content)
		if len(text) > MaxFileContentBudget {
			text = text[:MaxFileContentBudget] + "\n... (truncated)"
		}
		files = append(files, ContextFile{
			Path:    resolved,
			Content: text,
			Hash:    hash,
		})
	}
	return files
}

// fetchURL fetches content from a URL with a timeout.
func fetchURL(url string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(MaxFileContentBudget*2)))
	if err != nil {
		return ""
	}
	return string(body)
}
