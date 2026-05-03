package fileops

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileParams configures file writing.
type WriteFileParams struct {
	Path    string `json:"file_path"`
	Content string `json:"content"`
}

// WriteFile writes content to a file, creating parent directories as needed.
// If the file already exists, preserves its BOM and line ending style.
func WriteFile(params WriteFileParams, workspaceRoot string) error {
	// Validate path is within workspace.
	absPath, err := filepath.Abs(params.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if workspaceRoot != "" {
		absRoot, _ := filepath.Abs(workspaceRoot)
		rel, err := filepath.Rel(absRoot, absPath)
		if err != nil || len(rel) > 2 && rel[:2] == ".." {
			return fmt.Errorf("path %s is outside workspace %s", absPath, absRoot)
		}
	}

	// Create parent directories.
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Check file size limit.
	if len(params.Content) > MaxFileSize {
		return fmt.Errorf("content too large (%d bytes, max %d)", len(params.Content), MaxFileSize)
	}

	content := params.Content

	// If file exists, detect and preserve its encoding and line endings.
	existing, readErr := os.ReadFile(absPath)
	if readErr == nil && len(existing) > 0 {
		enc := DetectEncoding(existing)
		le := DetectLineEndings(existing)

		// Normalize content to match existing file's line endings.
		content = NormalizeLineEndings(content, le)

		// Preserve BOM if original had one.
		if enc == EncodingUTF8BOM {
			content = "\xEF\xBB\xBF" + content
		}
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", absPath, err)
	}

	return nil
}
