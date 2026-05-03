// Package fileops provides file operations including search and traversal.
package fileops

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// DefaultSkipDirs is the unified set of directory names to skip during walks.
// Used by grep, glob, indexer, and embedder to ensure consistent behavior.
var DefaultSkipDirs = map[string]bool{
	// VCS
	".git": true, ".hg": true, ".svn": true,
	// Package managers / build
	"node_modules": true, "vendor": true, "__pycache__": true,
	"dist": true, "build": true, "target": true, "bin": true,
	// Agent / tool state
	".agents": true, ".claw": true, ".claude": true,
	// Project-specific
	"priorart": true,
}

// ShouldSkipDir reports whether a directory with the given base name should
// be skipped during file walks. It checks the DefaultSkipDirs set and also
// skips hidden directories (dot-prefixed).
func ShouldSkipDir(name string) bool {
	if DefaultSkipDirs[name] {
		return true
	}
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	return false
}

// WalkOptions configures a WalkSourceFiles traversal.
type WalkOptions struct {
	// MaxFileSize is the maximum file size to visit (0 = no limit).
	MaxFileSize int64
	// SkipBinary skips files detected as binary.
	SkipBinary bool
}

// WalkSourceFiles walks a directory tree calling fn for each regular file,
// skipping directories in DefaultSkipDirs, hidden directories, and files
// matched by the IgnoreChecker (loaded from .ycodeignore or .gitignore).
//
// The fn callback receives absolute paths. Return filepath.SkipDir or
// filepath.SkipAll from fn to control traversal.
func WalkSourceFiles(root string, opts *WalkOptions, fn func(path string, d fs.DirEntry) error) error {
	ic := NewIgnoreChecker(root)

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}

		name := d.Name()

		if d.IsDir() {
			if path == root {
				return nil // always enter the root
			}
			if ShouldSkipDir(name) {
				return filepath.SkipDir
			}
			// Skip OS-protected directories (macOS TCC) to avoid permission dialogs.
			if IsProtectedPath(path) {
				return filepath.SkipDir
			}
			if ic != nil && ic.IsIgnored(path) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip ignored files.
		if ic != nil && ic.IsIgnored(path) {
			return nil
		}

		// Apply size limit.
		if opts != nil && opts.MaxFileSize > 0 {
			info, infoErr := d.Info()
			if infoErr != nil || info.Size() > opts.MaxFileSize || info.Size() == 0 {
				return nil
			}
		}

		// Skip binary files.
		if opts != nil && opts.SkipBinary {
			if bin, _ := IsBinaryFile(path); bin {
				return nil
			}
		}

		return fn(path, d)
	})
}

// SourceExtensions is the set of file extensions considered source code.
// Used by the indexer and embedder for filtering.
var SourceExtensions = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".java": true, ".c": true, ".cpp": true, ".cc": true, ".cxx": true,
	".h": true, ".hpp": true, ".hxx": true,
	".rb": true, ".sh": true, ".bash": true, ".zsh": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".md": true, ".txt": true, ".sql": true, ".graphql": true,
	".css": true, ".scss": true, ".html": true, ".xml": true,
	".proto": true, ".swift": true, ".kt": true, ".scala": true,
}

// IsSourceExt reports whether ext (including the dot) is a recognized source extension.
func IsSourceExt(ext string) bool {
	return SourceExtensions[strings.ToLower(ext)]
}
