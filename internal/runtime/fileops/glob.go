package fileops

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
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
	err := WalkSourceFiles(base, nil, func(path string, d fs.DirEntry) error {
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		// Use forward slashes for consistent matching.
		rel = filepath.ToSlash(rel)

		matched, matchErr := doublestar.Match(params.Pattern, rel)
		if matchErr != nil {
			return nil
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
