package fileops

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GrepOutputMode controls what grep returns.
type GrepOutputMode string

const (
	GrepOutputContent          GrepOutputMode = "content"
	GrepOutputFilesWithMatches GrepOutputMode = "files_with_matches"
	GrepOutputCount            GrepOutputMode = "count"
)

// GrepParams configures grep search.
type GrepParams struct {
	Pattern    string         `json:"pattern"`
	Path       string         `json:"path,omitempty"`
	Glob       string         `json:"glob,omitempty"`        // file glob filter
	Type       string         `json:"type,omitempty"`        // file type filter (go, py, js, etc.)
	OutputMode GrepOutputMode `json:"output_mode,omitempty"` // default: files_with_matches
	Context    int            `json:"context,omitempty"`     // context lines (-C)
	Before     int            `json:"-B,omitempty"`          // lines before (-B)
	After      int            `json:"-A,omitempty"`          // lines after (-A)
	HeadLimit  int            `json:"head_limit,omitempty"`  // max results
	Offset     int            `json:"offset,omitempty"`      // skip first N results
	IgnoreCase bool           `json:"-i,omitempty"`
}

// GrepMatch is a single matching or context line.
type GrepMatch struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Content   string `json:"content"`
	IsContext bool   `json:"is_context,omitempty"` // true for context lines (non-matching)
}

// GrepResult holds grep results.
type GrepResult struct {
	Matches []GrepMatch `json:"matches,omitempty"`
	Files   []string    `json:"files,omitempty"`
	Count   int         `json:"count,omitempty"`
}

// typeExtensions maps file types to extensions.
var typeExtensions = map[string][]string{
	"go":   {".go"},
	"py":   {".py"},
	"js":   {".js"},
	"ts":   {".ts", ".tsx"},
	"rust": {".rs"},
	"java": {".java"},
	"c":    {".c", ".h"},
	"cpp":  {".cpp", ".cc", ".cxx", ".hpp", ".hxx"},
	"rb":   {".rb"},
	"sh":   {".sh", ".bash", ".zsh"},
	"yaml": {".yaml", ".yml"},
	"json": {".json"},
	"md":   {".md"},
	"html": {".html", ".htm"},
	"css":  {".css"},
}

// GrepSearch searches file contents for a regex pattern.
func GrepSearch(params GrepParams) (*GrepResult, error) {
	flags := ""
	if params.IgnoreCase {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + params.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	base := params.Path
	if base == "" {
		base, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	mode := params.OutputMode
	if mode == "" {
		mode = GrepOutputFilesWithMatches
	}

	headLimit := params.HeadLimit
	if headLimit <= 0 {
		headLimit = 100
	}

	result := &GrepResult{}
	fileSet := make(map[string]bool)
	skipped := 0
	offset := params.Offset

	err = WalkSourceFiles(base, nil, func(path string, d fs.DirEntry) error {
		name := d.Name()

		// Apply type filter.
		if params.Type != "" {
			exts, ok := typeExtensions[params.Type]
			if ok {
				matched := false
				for _, ext := range exts {
					if strings.HasSuffix(name, ext) {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}
		}

		// Apply glob filter.
		if params.Glob != "" {
			matched, _ := filepath.Match(params.Glob, name)
			if !matched {
				return nil
			}
		}

		// Resolve context line counts.
		before := params.Before
		after := params.After
		if params.Context > 0 {
			if before == 0 {
				before = params.Context
			}
			if after == 0 {
				after = params.Context
			}
		}

		// Search file. Account for remaining offset + remaining result budget.
		remaining := offset - skipped + headLimit - len(result.Matches)
		if remaining <= 0 {
			return filepath.SkipAll
		}
		matches, err := searchFile(path, re, mode, remaining, before, after)
		if err != nil {
			return nil // skip files that can't be read
		}

		if len(matches) > 0 {
			switch mode {
			case GrepOutputFilesWithMatches:
				if !fileSet[path] {
					if skipped < offset {
						skipped++
					} else {
						fileSet[path] = true
						result.Files = append(result.Files, path)
					}
				}
			case GrepOutputContent:
				for _, m := range matches {
					if skipped < offset {
						skipped++
						continue
					}
					result.Matches = append(result.Matches, m)
				}
			case GrepOutputCount:
				result.Count += len(matches)
			}
		}

		if len(result.Matches) >= headLimit || len(result.Files) >= headLimit {
			return filepath.SkipAll
		}

		return nil
	})

	return result, err
}

// searchFile searches a single file for pattern matches with optional context.
func searchFile(path string, re *regexp.Regexp, mode GrepOutputMode, limit, before, after int) ([]GrepMatch, error) {
	needContext := (before > 0 || after > 0) && mode == GrepOutputContent

	if needContext {
		return searchFileWithContext(path, re, limit, before, after)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []GrepMatch
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, GrepMatch{
				File:    path,
				Line:    lineNum,
				Content: line,
			})
			if len(matches) >= limit {
				break
			}
		}
	}

	return matches, scanner.Err()
}

// searchFileWithContext reads the entire file and emits matches with
// surrounding context lines. Overlapping context windows are merged.
func searchFileWithContext(path string, re *regexp.Regexp, limit, before, after int) ([]GrepMatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty line from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Find all matching line indices (0-based).
	var matchLines []int
	for i, line := range lines {
		if re.MatchString(line) {
			matchLines = append(matchLines, i)
		}
	}

	if len(matchLines) == 0 {
		return nil, nil
	}

	// Build merged context windows to avoid duplicate lines.
	type window struct{ start, end int } // inclusive range
	var windows []window
	for _, idx := range matchLines {
		start := idx - before
		if start < 0 {
			start = 0
		}
		end := idx + after
		if end >= len(lines) {
			end = len(lines) - 1
		}

		// Merge with previous window if overlapping or adjacent.
		if len(windows) > 0 && start <= windows[len(windows)-1].end+1 {
			windows[len(windows)-1].end = end
		} else {
			windows = append(windows, window{start, end})
		}
	}

	// Build a set of matching lines for quick lookup.
	matchSet := make(map[int]bool, len(matchLines))
	for _, idx := range matchLines {
		matchSet[idx] = true
	}

	// Emit matches from merged windows.
	var matches []GrepMatch
	for _, w := range windows {
		for i := w.start; i <= w.end; i++ {
			matches = append(matches, GrepMatch{
				File:      path,
				Line:      i + 1, // 1-based
				Content:   lines[i],
				IsContext: !matchSet[i],
			})
			if len(matches) >= limit {
				return matches, nil
			}
		}
	}

	return matches, nil
}
