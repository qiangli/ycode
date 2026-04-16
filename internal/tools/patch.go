package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// RegisterPatchHandler registers the apply_patch tool handler.
func RegisterPatchHandler(r *Registry, v *vfs.VFS) {
	spec, ok := r.Get("apply_patch")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Patch string `json:"patch"`
			Strip int    `json:"strip"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse apply_patch input: %w", err)
		}
		if params.Patch == "" {
			return "", fmt.Errorf("patch content is required")
		}
		// Auto-detect Codex-style patch format (*** Begin Patch).
		if IsCodexPatch(params.Patch) {
			parsed, err := ParseCodexPatch(params.Patch)
			if err != nil {
				return "", fmt.Errorf("parse codex patch: %w", err)
			}
			return ApplyCodexPatch(ctx, v, parsed)
		}
		return applyPatch(ctx, v, params.Patch, params.Strip)
	}
}

// patchHunk represents a single hunk in a unified diff.
type patchHunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []string // raw hunk lines (starting with ' ', '+', '-')
}

// patchFile represents all hunks for a single file in a unified diff.
type patchFile struct {
	oldPath string
	newPath string
	hunks   []*patchHunk
	isNew   bool // --- /dev/null
	isDel   bool // +++ /dev/null
}

// applyPatch parses and applies a unified diff.
func applyPatch(ctx context.Context, v *vfs.VFS, patch string, strip int) (string, error) {
	files, err := parsePatch(patch, strip)
	if err != nil {
		return "", fmt.Errorf("parse patch: %w", err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no files found in patch")
	}

	var results []string
	for _, pf := range files {
		result, err := applyPatchFile(ctx, v, pf)
		if err != nil {
			return "", err
		}
		results = append(results, result)
	}
	return strings.Join(results, "\n"), nil
}

// stripPath removes n leading path components from a path.
func stripPath(p string, n int) string {
	if n <= 0 {
		return p
	}
	parts := strings.SplitN(p, "/", n+1)
	if len(parts) <= n {
		return parts[len(parts)-1]
	}
	return parts[n]
}

// parsePatch parses a unified diff into a list of per-file patches.
func parsePatch(patch string, strip int) ([]*patchFile, error) {
	var files []*patchFile
	scanner := bufio.NewScanner(strings.NewReader(patch))
	var current *patchFile
	var currentHunk *patchHunk

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "--- ") {
			// Start of a new file pair. Read the +++ line next.
			oldPath := strings.TrimPrefix(line, "--- ")
			// Remove timestamp if present (e.g., "--- a/file.go\t2024-01-01")
			if idx := strings.Index(oldPath, "\t"); idx >= 0 {
				oldPath = oldPath[:idx]
			}
			oldPath = strings.TrimSpace(oldPath)

			if !scanner.Scan() {
				return nil, fmt.Errorf("unexpected end of patch after ---")
			}
			plusLine := scanner.Text()
			if !strings.HasPrefix(plusLine, "+++ ") {
				return nil, fmt.Errorf("expected +++ line, got: %s", plusLine)
			}
			newPath := strings.TrimPrefix(plusLine, "+++ ")
			if idx := strings.Index(newPath, "\t"); idx >= 0 {
				newPath = newPath[:idx]
			}
			newPath = strings.TrimSpace(newPath)

			// Strip leading path components.
			isNew := oldPath == "/dev/null"
			isDel := newPath == "/dev/null"
			if !isNew {
				oldPath = stripPath(oldPath, strip)
			}
			if !isDel {
				newPath = stripPath(newPath, strip)
			}

			current = &patchFile{
				oldPath: oldPath,
				newPath: newPath,
				isNew:   isNew,
				isDel:   isDel,
			}
			currentHunk = nil
			files = append(files, current)
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			if current == nil {
				return nil, fmt.Errorf("hunk header without file header: %s", line)
			}
			hunk, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			currentHunk = hunk
			current.hunks = append(current.hunks, currentHunk)
			continue
		}

		// Hunk content lines.
		if currentHunk != nil && len(line) > 0 {
			ch := line[0]
			if ch == ' ' || ch == '+' || ch == '-' {
				currentHunk.lines = append(currentHunk.lines, line)
				continue
			}
		}
		// Allow empty context lines within a hunk (represented as empty string).
		if currentHunk != nil && line == "" {
			currentHunk.lines = append(currentHunk.lines, " ")
			continue
		}
		// Skip other lines (diff --git, index, etc.).
	}

	return files, scanner.Err()
}

// parseHunkHeader parses a unified diff hunk header like "@@ -1,3 +1,4 @@".
func parseHunkHeader(line string) (*patchHunk, error) {
	// Format: @@ -old_start[,old_count] +new_start[,new_count] @@
	parts := strings.SplitN(line, "@@", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid hunk header: %s", line)
	}
	rangeStr := strings.TrimSpace(parts[1])
	rangeParts := strings.Fields(rangeStr)
	if len(rangeParts) < 2 {
		return nil, fmt.Errorf("invalid hunk range: %s", rangeStr)
	}

	oldStart, oldCount, err := parseRange(rangeParts[0])
	if err != nil {
		return nil, fmt.Errorf("parse old range %q: %w", rangeParts[0], err)
	}
	newStart, newCount, err := parseRange(rangeParts[1])
	if err != nil {
		return nil, fmt.Errorf("parse new range %q: %w", rangeParts[1], err)
	}

	return &patchHunk{
		oldStart: oldStart,
		oldCount: oldCount,
		newStart: newStart,
		newCount: newCount,
	}, nil
}

// parseRange parses "-start,count" or "+start,count" (count defaults to 1).
func parseRange(s string) (start, count int, err error) {
	s = strings.TrimLeft(s, "+-")
	parts := strings.SplitN(s, ",", 2)
	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	if len(parts) == 2 {
		count, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, err
		}
	} else {
		count = 1
	}
	return start, count, nil
}

// applyPatchFile applies all hunks for a single file.
func applyPatchFile(ctx context.Context, v *vfs.VFS, pf *patchFile) (string, error) {
	targetPath := pf.newPath
	if pf.isDel {
		targetPath = pf.oldPath
	}

	absPath, err := v.ValidatePath(ctx, targetPath)
	if err != nil {
		return "", fmt.Errorf("validate path %s: %w", targetPath, err)
	}

	// Handle file deletion.
	if pf.isDel {
		if err := os.Remove(absPath); err != nil {
			return "", fmt.Errorf("delete %s: %w", absPath, err)
		}
		return fmt.Sprintf("deleted %s", absPath), nil
	}

	// Read existing content (empty for new files).
	var lines []string
	if !pf.isNew {
		data, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", absPath, err)
		}
		content := string(data)
		if content == "" {
			lines = []string{}
		} else {
			lines = strings.Split(content, "\n")
		}
	}

	// Apply hunks in reverse order so line numbers stay valid.
	for i := len(pf.hunks) - 1; i >= 0; i-- {
		hunk := pf.hunks[i]
		lines, err = applyHunk(lines, hunk, absPath)
		if err != nil {
			return "", err
		}
	}

	// Write result.
	result := strings.Join(lines, "\n")
	// Create parent directories for new files.
	if pf.isNew {
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(absPath, []byte(result), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", absPath, err)
	}

	action := "patched"
	if pf.isNew {
		action = "created"
	}
	return fmt.Sprintf("%s %s", action, absPath), nil
}

// applyHunk applies a single hunk to a set of lines.
func applyHunk(lines []string, hunk *patchHunk, filePath string) ([]string, error) {
	// Convert 1-based hunk line numbers to 0-based index.
	startIdx := hunk.oldStart - 1
	if startIdx < 0 {
		startIdx = 0
	}

	// Verify context lines match.
	oldLineIdx := startIdx
	for _, hl := range hunk.lines {
		if len(hl) == 0 {
			continue
		}
		switch hl[0] {
		case ' ':
			// Context line — must match.
			content := hl[1:]
			if oldLineIdx >= len(lines) {
				return nil, fmt.Errorf("hunk failed at %s line %d: unexpected end of file", filePath, oldLineIdx+1)
			}
			if lines[oldLineIdx] != content {
				return nil, fmt.Errorf("hunk failed at %s line %d: context mismatch\n  expected: %q\n  got:      %q",
					filePath, oldLineIdx+1, content, lines[oldLineIdx])
			}
			oldLineIdx++
		case '-':
			// Removed line — must match.
			content := hl[1:]
			if oldLineIdx >= len(lines) {
				return nil, fmt.Errorf("hunk failed at %s line %d: unexpected end of file for removal", filePath, oldLineIdx+1)
			}
			if lines[oldLineIdx] != content {
				return nil, fmt.Errorf("hunk failed at %s line %d: removal mismatch\n  expected: %q\n  got:      %q",
					filePath, oldLineIdx+1, content, lines[oldLineIdx])
			}
			oldLineIdx++
		case '+':
			// Added line — skip for verification.
		}
	}

	// Build new lines for this region.
	var newSection []string
	for _, hl := range hunk.lines {
		if len(hl) == 0 {
			continue
		}
		switch hl[0] {
		case ' ':
			newSection = append(newSection, hl[1:])
		case '+':
			newSection = append(newSection, hl[1:])
		case '-':
			// Removed — skip.
		}
	}

	// Replace the old range with the new section.
	endIdx := startIdx + hunk.oldCount
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	result := make([]string, 0, len(lines)-hunk.oldCount+len(newSection))
	result = append(result, lines[:startIdx]...)
	result = append(result, newSection...)
	result = append(result, lines[endIdx:]...)
	return result, nil
}
