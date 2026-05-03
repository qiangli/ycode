package fileops

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultReadLimit is the default number of lines to read.
	DefaultReadLimit = 2000
	// MaxFileSize is the maximum file size to read (10 MB).
	MaxFileSize = 10 * 1024 * 1024
	// MaxReadOutputBytes caps inline output size (50 KB, matching OpenCode).
	// Prevents pathologically large file reads from bloating conversation context.
	MaxReadOutputBytes = 50 * 1024
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
		if errors.Is(err, os.ErrNotExist) {
			suggestion := suggestSimilarFiles(params.Path)
			if suggestion != "" {
				return "", fmt.Errorf("file not found: %s\n%s", params.Path, suggestion)
			}
		}
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

	// Cap output size to prevent bloating conversation context.
	if b.Len() > MaxReadOutputBytes {
		output := b.String()[:MaxReadOutputBytes]
		// Find the last newline to avoid cutting mid-line.
		if idx := strings.LastIndex(output, "\n"); idx > 0 {
			output = output[:idx+1]
		}
		nextOffset := params.Offset + linesRead
		return output + fmt.Sprintf("\n[Output truncated at %dKB. Use offset=%d to continue reading.]",
			MaxReadOutputBytes/1024, nextOffset), nil
	}

	return b.String(), nil
}

// suggestSimilarFiles looks for files in the same directory with names similar
// to the requested file. Returns a formatted suggestion string, or empty if
// no good matches are found.
func suggestSimilarFiles(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	type scored struct {
		name  string
		score int
	}

	var candidates []scored
	baseLower := strings.ToLower(base)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		nameLower := strings.ToLower(name)

		// Score by edit distance and prefix/substring matching.
		score := 0
		if strings.HasPrefix(nameLower, baseLower[:min(len(baseLower), 3)]) {
			score += 3
		}
		if strings.Contains(nameLower, baseLower) || strings.Contains(baseLower, nameLower) {
			score += 5
		}
		// Same extension bonus.
		if filepath.Ext(name) == filepath.Ext(base) {
			score += 2
		}
		// Edit distance for short names.
		if len(base) < 50 && len(name) < 50 {
			dist := levenshtein(baseLower, nameLower)
			if dist <= 3 {
				score += 4 - dist
			}
		}

		if score > 0 {
			candidates = append(candidates, scored{name, score})
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	limit := min(3, len(candidates))
	var sb strings.Builder
	sb.WriteString("Did you mean one of these files?\n")
	for i := 0; i < limit; i++ {
		sb.WriteString("  - ")
		sb.WriteString(filepath.Join(dir, candidates[i].name))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
