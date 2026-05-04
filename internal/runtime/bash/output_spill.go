package bash

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SpillConfig configures the output disk spill behavior.
type SpillConfig struct {
	// Threshold in bytes. Output exceeding this is spilled to disk.
	Threshold int // default 512KB
	// Dir is the directory for spill files. Defaults to os.TempDir()/ycode-spill.
	Dir string
	// Retention is how long spill files are kept. Default 7 days.
	Retention time.Duration
	// PreviewLines is the number of tail lines to keep in the truncated preview.
	PreviewLines int // default 50
}

// DefaultSpillConfig returns sensible defaults.
func DefaultSpillConfig() SpillConfig {
	return SpillConfig{
		Threshold:    512 * 1024, // 512KB
		Dir:          filepath.Join(os.TempDir(), "ycode-spill"),
		Retention:    7 * 24 * time.Hour,
		PreviewLines: 50,
	}
}

// SpillResult holds the outcome of a spill operation.
type SpillResult struct {
	// Spilled is true if the output was written to disk.
	Spilled bool
	// FilePath is the path to the spill file (empty if not spilled).
	FilePath string
	// Preview is the truncated tail of the output.
	Preview string
	// FullSize is the total output size in bytes.
	FullSize int
}

// OutputSpiller manages disk spilling of large tool outputs.
// Inspired by opencode's dual-buffer (memory + disk) approach with
// auto-cleanup retention.
type OutputSpiller struct {
	config SpillConfig
	mu     sync.Mutex
}

// NewOutputSpiller creates a spiller with the given config.
func NewOutputSpiller(cfg SpillConfig) *OutputSpiller {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultSpillConfig().Threshold
	}
	if cfg.Dir == "" {
		cfg.Dir = DefaultSpillConfig().Dir
	}
	if cfg.Retention <= 0 {
		cfg.Retention = DefaultSpillConfig().Retention
	}
	if cfg.PreviewLines <= 0 {
		cfg.PreviewLines = DefaultSpillConfig().PreviewLines
	}
	return &OutputSpiller{config: cfg}
}

// Spill checks if the output exceeds the threshold and writes to disk if so.
// Returns a SpillResult with either the full output (if small) or a preview + file path.
func (s *OutputSpiller) Spill(output []byte) (SpillResult, error) {
	result := SpillResult{FullSize: len(output)}

	if len(output) <= s.config.Threshold {
		result.Preview = string(output)
		return result, nil
	}

	// Ensure spill directory exists.
	s.mu.Lock()
	err := os.MkdirAll(s.config.Dir, 0o700)
	s.mu.Unlock()
	if err != nil {
		return result, fmt.Errorf("create spill dir: %w", err)
	}

	// Write to temp file in spill directory.
	f, err := os.CreateTemp(s.config.Dir, "tool_*.txt")
	if err != nil {
		return result, fmt.Errorf("create spill file: %w", err)
	}

	if _, err := f.Write(output); err != nil {
		f.Close()
		return result, fmt.Errorf("write spill file: %w", err)
	}
	if err := f.Close(); err != nil {
		return result, fmt.Errorf("close spill file: %w", err)
	}

	result.Spilled = true
	result.FilePath = f.Name()
	result.Preview = tailLines(string(output), s.config.PreviewLines)

	return result, nil
}

// Cleanup removes spill files older than the retention period.
func (s *OutputSpiller) Cleanup() (int, error) {
	entries, err := os.ReadDir(s.config.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	cutoff := time.Now().Add(-s.config.Retention)
	removed := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(s.config.Dir, entry.Name())
			if os.Remove(path) == nil {
				removed++
			}
		}
	}

	return removed, nil
}

// tailLines returns the last n lines of text.
func tailLines(text string, n int) string {
	if n <= 0 {
		return text
	}

	// Walk backward, counting newlines. We want n non-empty lines.
	// Skip trailing newline if present.
	end := len(text)
	if end > 0 && text[end-1] == '\n' {
		end--
	}

	lines := 0
	pos := end
	for pos > 0 {
		pos--
		if text[pos] == '\n' {
			lines++
			if lines >= n {
				pos++ // skip past the newline
				break
			}
		}
	}

	if pos > 0 {
		return fmt.Sprintf("[... %d bytes truncated, full output saved to disk ...]\n", pos) + text[pos:]
	}
	return text
}
