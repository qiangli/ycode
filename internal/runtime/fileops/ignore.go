package fileops

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreChecker checks file paths against .ycodeignore patterns.
type IgnoreChecker struct {
	baseDir  string
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern  string
	negation bool
	dirOnly  bool
}

// NewIgnoreChecker creates an IgnoreChecker by reading .ycodeignore from the
// given directory. If .ycodeignore doesn't exist, it falls back to .gitignore.
// Returns nil if neither file exists.
func NewIgnoreChecker(dir string) *IgnoreChecker {
	patterns := loadIgnoreFile(filepath.Join(dir, ".ycodeignore"))
	if patterns == nil {
		patterns = loadIgnoreFile(filepath.Join(dir, ".gitignore"))
	}
	if patterns == nil {
		return nil
	}
	return &IgnoreChecker{
		baseDir:  dir,
		patterns: patterns,
	}
}

// loadIgnoreFile reads patterns from a file. Returns nil if the file
// doesn't exist or can't be read.
func loadIgnoreFile(path string) []ignorePattern {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []ignorePattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := ignorePattern{}

		if strings.HasPrefix(line, "!") {
			p.negation = true
			line = line[1:]
		}

		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		p.pattern = line
		patterns = append(patterns, p)
	}

	return patterns
}

// IsIgnored returns true if the given path should be ignored.
// The path should be relative to the base directory or absolute.
func (ic *IgnoreChecker) IsIgnored(path string) bool {
	if ic == nil {
		return false
	}

	// Make path relative to baseDir if absolute.
	rel := path
	if filepath.IsAbs(path) {
		var err error
		rel, err = filepath.Rel(ic.baseDir, path)
		if err != nil {
			return false
		}
	}

	// Normalize to forward slashes for consistent matching.
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)

	ignored := false
	for _, p := range ic.patterns {
		matched := matchPattern(p.pattern, rel, base, p.dirOnly, path)
		if matched {
			if p.negation {
				ignored = false
			} else {
				ignored = true
			}
		}
	}

	return ignored
}

// matchPattern checks if a path matches an ignore pattern.
func matchPattern(pattern, relPath, baseName string, dirOnly bool, absPath string) bool {
	// If dirOnly, check that the path is actually a directory.
	if dirOnly {
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			return false
		}
	}

	// Try matching against the base name.
	if matched, _ := filepath.Match(pattern, baseName); matched {
		return true
	}

	// Try matching against the full relative path.
	if matched, _ := filepath.Match(pattern, relPath); matched {
		return true
	}

	return false
}
