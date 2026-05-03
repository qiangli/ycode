package bash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// MaxSavedOutputBytes is the maximum size for saved tool output files (64 MiB).
	MaxSavedOutputBytes = 64 * 1024 * 1024

	// OutputPreviewHeadLines is the number of head lines in truncated output.
	OutputPreviewHeadLines = 20

	// OutputPreviewTailLines is the number of tail lines in truncated output.
	OutputPreviewTailLines = 10

	// OutputSaveThresholdBytes is the byte threshold above which output is
	// saved to disk and replaced with a preview.
	OutputSaveThresholdBytes = 50 * 1024 // 50 KB
)

// SaveLargeOutput saves command output to disk when it exceeds the inline
// threshold. Returns a preview string with the saved file path, or the
// original output if it's small enough.
//
// Inspired by Claude Code's BashTool large output handling.
func SaveLargeOutput(output string, toolName string, outputDir string) string {
	if len(output) <= OutputSaveThresholdBytes {
		return output
	}

	if outputDir == "" {
		return truncateLargeOutput(output)
	}

	// Save to disk.
	savedPath := saveOutputToDisk(output, toolName, outputDir)
	if savedPath == "" {
		return truncateLargeOutput(output)
	}

	return formatOutputPreview(output, savedPath)
}

// saveOutputToDisk writes the full output to a file and returns the path.
func saveOutputToDisk(output string, toolName string, dir string) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}

	// Truncate if exceeding max size.
	data := []byte(output)
	if len(data) > MaxSavedOutputBytes {
		data = data[:MaxSavedOutputBytes]
	}

	ts := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s.txt", toolName, ts)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return path
}

// formatOutputPreview creates a preview of large output with a file path reference.
func formatOutputPreview(output string, savedPath string) string {
	lines := strings.Split(output, "\n")
	totalLines := len(lines)

	var b strings.Builder

	headN := OutputPreviewHeadLines
	tailN := OutputPreviewTailLines
	if totalLines <= headN+tailN {
		headN = totalLines / 2
		tailN = totalLines - headN
	}

	for i := 0; i < headN && i < totalLines; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}

	omitted := totalLines - headN - tailN
	if omitted > 0 {
		fmt.Fprintf(&b, "\n[... %d lines omitted, full output saved to %s ...]\n", omitted, savedPath)
		b.WriteString("Use Read with file_path to access the full output, or Grep to search it.\n\n")
	}

	for i := totalLines - tailN; i < totalLines; i++ {
		if i >= 0 {
			b.WriteString(lines[i])
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// truncateLargeOutput truncates output to head + tail when disk save is unavailable.
func truncateLargeOutput(output string) string {
	lines := strings.Split(output, "\n")
	totalLines := len(lines)
	headN := OutputPreviewHeadLines
	tailN := OutputPreviewTailLines

	if totalLines <= headN+tailN {
		return output
	}

	var b strings.Builder
	for i := 0; i < headN; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}

	omitted := totalLines - headN - tailN
	fmt.Fprintf(&b, "\n[... %d lines omitted ...]\n\n", omitted)

	for i := totalLines - tailN; i < totalLines; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}

	return b.String()
}
