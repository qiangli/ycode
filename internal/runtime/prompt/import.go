package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// MaxImportDepth is the maximum nesting depth for #import directives.
	MaxImportDepth = 3
	// importPrefix is the directive marker.
	importPrefix = "#import "
)

// ResolveImports processes #import directives in instruction file content.
// Each #import <relative-path> line is replaced with the file's content.
// Circular references are detected and marked inline.
// basePath is the directory of the file containing the directives.
func ResolveImports(content string, basePath string, visited map[string]bool, depth int) string {
	if depth > MaxImportDepth {
		return content
	}

	var result strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, importPrefix) {
			result.WriteString(line)
			result.WriteByte('\n')
			continue
		}

		// Extract the import path.
		importPath := strings.TrimSpace(strings.TrimPrefix(trimmed, importPrefix))
		importPath = strings.Trim(importPath, "\"'<>")

		if importPath == "" {
			result.WriteString(line)
			result.WriteByte('\n')
			continue
		}

		// Resolve relative to the containing file's directory.
		absPath := importPath
		if !filepath.IsAbs(importPath) {
			absPath = filepath.Join(basePath, importPath)
		}
		absPath = filepath.Clean(absPath)

		// Check for circular imports.
		if visited[absPath] {
			result.WriteString("<!-- circular import: ")
			result.WriteString(importPath)
			result.WriteString(" -->\n")
			continue
		}

		// Read the imported file.
		data, err := os.ReadFile(absPath)
		if err != nil {
			result.WriteString("<!-- import not found: ")
			result.WriteString(importPath)
			result.WriteString(" -->\n")
			continue
		}

		// Mark as visited and recurse.
		visited[absPath] = true
		resolved := ResolveImports(string(data), filepath.Dir(absPath), visited, depth+1)
		result.WriteString(resolved)
		// Ensure trailing newline.
		if !strings.HasSuffix(resolved, "\n") {
			result.WriteByte('\n')
		}
	}

	return strings.TrimRight(result.String(), "\n") + "\n"
}
