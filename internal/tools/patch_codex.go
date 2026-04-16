package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// CodexPatch represents a parsed Codex-style patch.
type CodexPatch struct {
	Hunks []CodexHunk
}

// CodexHunkOp is the type of file operation.
type CodexHunkOp int

const (
	CodexHunkAdd    CodexHunkOp = iota // *** Add File:
	CodexHunkDelete                    // *** Delete File:
	CodexHunkUpdate                    // *** Update File:
)

// CodexHunk represents a single file operation in a Codex patch.
type CodexHunk struct {
	Op       CodexHunkOp
	Path     string
	MoveTo   string // *** Move to: (for renames)
	Changes  []CodexChange
	AddLines []string // For Add operations: lines to write (without + prefix)
}

// CodexChange represents a single change block within an Update hunk.
type CodexChange struct {
	ContextHint string // @@ hint (e.g., "class Foo" or "def method()")
	Lines       []CodexLine
}

// CodexLine is a single line in a change block.
type CodexLine struct {
	Op   byte // '+', '-', or ' '
	Text string
}

// IsCodexPatch checks if the input looks like a Codex-style patch.
func IsCodexPatch(input string) bool {
	trimmed := strings.TrimSpace(input)
	return strings.HasPrefix(trimmed, "*** Begin Patch")
}

// ParseCodexPatch parses a Codex-style patch string.
func ParseCodexPatch(input string) (*CodexPatch, error) {
	lines := strings.Split(input, "\n")
	patch := &CodexPatch{}

	i := 0
	// Find *** Begin Patch
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "*** Begin Patch" {
			i++
			break
		}
		i++
	}

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "*** End Patch" || trimmed == "" && i == len(lines)-1 {
			break
		}

		if strings.HasPrefix(line, "*** Add File: ") {
			path := strings.TrimPrefix(line, "*** Add File: ")
			path = strings.TrimSpace(path)
			hunk := CodexHunk{Op: CodexHunkAdd, Path: path}
			i++
			for i < len(lines) && strings.HasPrefix(lines[i], "+") {
				hunk.AddLines = append(hunk.AddLines, strings.TrimPrefix(lines[i], "+"))
				i++
			}
			patch.Hunks = append(patch.Hunks, hunk)
			continue
		}

		if strings.HasPrefix(line, "*** Delete File: ") {
			path := strings.TrimPrefix(line, "*** Delete File: ")
			path = strings.TrimSpace(path)
			patch.Hunks = append(patch.Hunks, CodexHunk{Op: CodexHunkDelete, Path: path})
			i++
			continue
		}

		if strings.HasPrefix(line, "*** Update File: ") {
			path := strings.TrimPrefix(line, "*** Update File: ")
			path = strings.TrimSpace(path)
			hunk := CodexHunk{Op: CodexHunkUpdate, Path: path}
			i++

			// Optional: *** Move to:
			if i < len(lines) && strings.HasPrefix(lines[i], "*** Move to: ") {
				hunk.MoveTo = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to: "))
				i++
			}

			// Parse changes (@@-delimited blocks)
			for i < len(lines) {
				if strings.HasPrefix(lines[i], "*** ") {
					break // Next file operation
				}
				if strings.HasPrefix(lines[i], "@@") {
					hint := ""
					if len(lines[i]) > 2 {
						hint = strings.TrimSpace(strings.TrimPrefix(lines[i], "@@"))
					}
					change := CodexChange{ContextHint: hint}
					i++
					for i < len(lines) {
						l := lines[i]
						if strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "*** ") {
							break
						}
						if len(l) == 0 {
							i++
							continue
						}
						op := l[0]
						if op == '+' || op == '-' || op == ' ' {
							text := ""
							if len(l) > 1 {
								text = l[1:]
							}
							change.Lines = append(change.Lines, CodexLine{Op: op, Text: text})
						}
						i++
					}
					hunk.Changes = append(hunk.Changes, change)
				} else {
					i++
				}
			}
			patch.Hunks = append(patch.Hunks, hunk)
			continue
		}

		i++ // Skip unrecognized lines
	}

	if len(patch.Hunks) == 0 {
		return nil, fmt.Errorf("no file operations found in patch")
	}

	return patch, nil
}

// ApplyCodexPatch applies a parsed Codex patch to the filesystem via VFS.
func ApplyCodexPatch(ctx context.Context, v *vfs.VFS, patch *CodexPatch) (string, error) {
	var results []string

	for _, hunk := range patch.Hunks {
		switch hunk.Op {
		case CodexHunkAdd:
			content := strings.Join(hunk.AddLines, "\n")
			if len(hunk.AddLines) > 0 {
				content += "\n"
			}
			absPath, err := resolveCodexPath(ctx, v, hunk.Path)
			if err != nil {
				return "", fmt.Errorf("resolve path %s: %w", hunk.Path, err)
			}
			dir := filepath.Dir(absPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("create directory %s: %w", dir, err)
			}
			if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("create file %s: %w", hunk.Path, err)
			}
			results = append(results, fmt.Sprintf("Created %s", hunk.Path))

		case CodexHunkDelete:
			absPath, err := resolveCodexPath(ctx, v, hunk.Path)
			if err != nil {
				return "", fmt.Errorf("resolve path %s: %w", hunk.Path, err)
			}
			if err := os.Remove(absPath); err != nil {
				return "", fmt.Errorf("delete file %s: %w", hunk.Path, err)
			}
			results = append(results, fmt.Sprintf("Deleted %s", hunk.Path))

		case CodexHunkUpdate:
			absPath, err := resolveCodexPath(ctx, v, hunk.Path)
			if err != nil {
				return "", fmt.Errorf("resolve path %s: %w", hunk.Path, err)
			}
			data, err := os.ReadFile(absPath)
			if err != nil {
				return "", fmt.Errorf("read file %s: %w", hunk.Path, err)
			}
			fileLines := strings.Split(string(data), "\n")

			for _, change := range hunk.Changes {
				fileLines, err = applyCodexChange(fileLines, change)
				if err != nil {
					return "", fmt.Errorf("apply change to %s: %w", hunk.Path, err)
				}
			}

			newContent := strings.Join(fileLines, "\n")

			// Handle rename.
			targetPath := absPath
			if hunk.MoveTo != "" {
				tp, err := resolveCodexPath(ctx, v, hunk.MoveTo)
				if err != nil {
					return "", fmt.Errorf("resolve move target %s: %w", hunk.MoveTo, err)
				}
				targetPath = tp
				dir := filepath.Dir(targetPath)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", fmt.Errorf("create directory for move %s: %w", dir, err)
				}
				_ = os.Remove(absPath)
			}

			if err := os.WriteFile(targetPath, []byte(newContent), 0o644); err != nil {
				return "", fmt.Errorf("write file %s: %w", hunk.Path, err)
			}
			if hunk.MoveTo != "" {
				results = append(results, fmt.Sprintf("Updated and moved %s → %s", hunk.Path, hunk.MoveTo))
			} else {
				results = append(results, fmt.Sprintf("Updated %s", hunk.Path))
			}
		}
	}

	return strings.Join(results, "\n"), nil
}

// resolveCodexPath resolves a relative path through VFS validation.
func resolveCodexPath(ctx context.Context, v *vfs.VFS, path string) (string, error) {
	return v.ValidatePath(ctx, path)
}

// applyCodexChange applies a single change block to file lines.
// Uses context matching to find the correct position in the file.
func applyCodexChange(fileLines []string, change CodexChange) ([]string, error) {
	// Collect context lines (lines with ' ' prefix) and old lines ('-' prefix)
	// to find where in the file this change applies.
	var contextAndOld []string
	for _, cl := range change.Lines {
		if cl.Op == ' ' || cl.Op == '-' {
			contextAndOld = append(contextAndOld, cl.Text)
		}
	}

	if len(contextAndOld) == 0 {
		// No context — append new lines at end.
		for _, cl := range change.Lines {
			if cl.Op == '+' {
				fileLines = append(fileLines, cl.Text)
			}
		}
		return fileLines, nil
	}

	// Find the position in fileLines where contextAndOld matches.
	matchIdx := findContextMatch(fileLines, contextAndOld, change.ContextHint)
	if matchIdx < 0 {
		return nil, fmt.Errorf("could not find context match for change (hint: %q)", change.ContextHint)
	}

	// Build new lines: replace the matched region with the change output.
	var result []string
	result = append(result, fileLines[:matchIdx]...)

	srcIdx := matchIdx
	for _, cl := range change.Lines {
		switch cl.Op {
		case ' ':
			// Context line — keep from source.
			if srcIdx < len(fileLines) {
				result = append(result, fileLines[srcIdx])
				srcIdx++
			}
		case '-':
			// Removed line — skip in source.
			srcIdx++
		case '+':
			// Added line — insert.
			result = append(result, cl.Text)
		}
	}

	// Append remaining file lines after the matched region.
	result = append(result, fileLines[srcIdx:]...)
	return result, nil
}

// findContextMatch finds where contextLines appear in fileLines.
// If hint is provided, narrows search to the region near the hint.
func findContextMatch(fileLines, contextLines []string, hint string) int {
	if len(contextLines) == 0 {
		return -1
	}

	// If there's a context hint, try to find it first to narrow the search.
	startSearch := 0
	if hint != "" {
		for i, line := range fileLines {
			if strings.Contains(strings.TrimSpace(line), strings.TrimSpace(hint)) {
				startSearch = i
				break
			}
		}
	}

	// Search from startSearch onward for the first context line.
	for i := startSearch; i <= len(fileLines)-len(contextLines); i++ {
		if matchesAt(fileLines, i, contextLines) {
			return i
		}
	}

	// If hint-based search failed, try from beginning.
	if startSearch > 0 {
		for i := 0; i < startSearch && i <= len(fileLines)-len(contextLines); i++ {
			if matchesAt(fileLines, i, contextLines) {
				return i
			}
		}
	}

	return -1
}

// matchesAt checks if contextLines match fileLines starting at position idx.
func matchesAt(fileLines []string, idx int, contextLines []string) bool {
	for j, cl := range contextLines {
		if idx+j >= len(fileLines) {
			return false
		}
		if strings.TrimRight(fileLines[idx+j], " \t") != strings.TrimRight(cl, " \t") {
			return false
		}
	}
	return true
}
