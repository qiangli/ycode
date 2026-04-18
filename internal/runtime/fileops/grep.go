package fileops

import (
	"bufio"
	"fmt"
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
	IgnoreCase bool           `json:"-i,omitempty"`
}

// GrepMatch is a single matching line.
type GrepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
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

	err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") && path != base {
				return filepath.SkipDir
			}
			if name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

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

		// Search file.
		matches, err := searchFile(path, re, mode, headLimit-len(result.Matches))
		if err != nil {
			return nil // skip files that can't be read
		}

		if len(matches) > 0 {
			switch mode {
			case GrepOutputFilesWithMatches:
				if !fileSet[path] {
					fileSet[path] = true
					result.Files = append(result.Files, path)
				}
			case GrepOutputContent:
				result.Matches = append(result.Matches, matches...)
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

// searchFile searches a single file for pattern matches.
func searchFile(path string, re *regexp.Regexp, mode GrepOutputMode, limit int) ([]GrepMatch, error) {
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
