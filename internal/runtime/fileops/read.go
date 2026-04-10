package fileops

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultReadLimit is the default number of lines to read.
	DefaultReadLimit = 2000
	// MaxFileSize is the maximum file size to read (10 MB).
	MaxFileSize = 10 * 1024 * 1024
)

// ReadFileParams configures file reading.
type ReadFileParams struct {
	Path   string `json:"file_path"`
	Offset int    `json:"offset,omitempty"` // 0-based line offset
	Limit  int    `json:"limit,omitempty"`  // number of lines to read
}

// ReadFile reads a file with optional offset and limit.
// Returns content with line numbers in "cat -n" format.
func ReadFile(params ReadFileParams) (string, error) {
	info, err := os.Stat(params.Path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", params.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file", params.Path)
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("file %s is too large (%d bytes, max %d)", params.Path, info.Size(), MaxFileSize)
	}

	f, err := os.Open(params.Path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", params.Path, err)
	}
	defer f.Close()

	limit := params.Limit
	if limit <= 0 {
		limit = DefaultReadLimit
	}

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	lineNum := 0
	linesRead := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= params.Offset {
			continue
		}
		if linesRead >= limit {
			break
		}

		line := scanner.Text()
		// Check for binary content.
		if linesRead == 0 && !utf8.ValidString(line) {
			return "", fmt.Errorf("file %s appears to be binary", params.Path)
		}

		fmt.Fprintf(&b, "%d\t%s\n", lineNum, line)
		linesRead++
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", params.Path, err)
	}

	if b.Len() == 0 {
		return fmt.Sprintf("(file %s is empty or offset is past end of file)", params.Path), nil
	}

	return b.String(), nil
}
