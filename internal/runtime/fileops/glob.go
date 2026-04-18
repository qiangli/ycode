package fileops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GlobParams configures glob search.
type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"` // base directory
}

// GlobResult holds matching file paths.
type GlobResult struct {
	Files []string `json:"files"`
}

// GlobSearch finds files matching a glob pattern.
func GlobSearch(params GlobParams) (*GlobResult, error) {
	base := params.Path
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	var matches []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}

		// Skip hidden directories (except the base).
		name := d.Name()
		if d.IsDir() && strings.HasPrefix(name, ".") && path != base {
			return filepath.SkipDir
		}
		// Skip node_modules, vendor, etc.
		if d.IsDir() && (name == "node_modules" || name == "vendor" || name == "__pycache__") {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		// Match against pattern.
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return nil
		}

		matched, err := filepath.Match(params.Pattern, filepath.Base(rel))
		if err != nil {
			return nil
		}

		// Also try matching the full relative path for patterns like "**/*.go".
		if !matched {
			matched, _ = filepath.Match(params.Pattern, rel)
		}

		// Simple ** support: if pattern contains **, try matching just the base name.
		if !matched && strings.Contains(params.Pattern, "**") {
			subPattern := strings.TrimPrefix(params.Pattern, "**/")
			matched, _ = filepath.Match(subPattern, filepath.Base(rel))
		}

		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort by modification time (newest first).
	sort.Slice(matches, func(i, j int) bool {
		infoI, _ := os.Stat(matches[i])
		infoJ, _ := os.Stat(matches[j])
		if infoI == nil || infoJ == nil {
			return matches[i] < matches[j]
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Cap results to prevent bloating conversation context.
	const maxGlobResults = 100
	if len(matches) > maxGlobResults {
		total := len(matches)
		matches = matches[:maxGlobResults]
		matches = append(matches,
			fmt.Sprintf("(Showing %d of %d matches. Narrow the pattern for more specific results.)", maxGlobResults, total))
	}

	return &GlobResult{Files: matches}, nil
}
