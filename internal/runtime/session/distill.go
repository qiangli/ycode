package session

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultMaxInlineChars is the default threshold for tool output distillation.
	DefaultMaxInlineChars = 2000
	// DefaultMaxInlineBytes is the byte-level threshold (50 KB, matching opencode).
	DefaultMaxInlineBytes = 50 * 1024
	// DefaultMaxInlineLines is the line-count threshold.
	DefaultMaxInlineLines = 2000
	// DefaultMaxLineLength caps individual line length (protects against minified files).
	DefaultMaxLineLength = 2000

	// Aggressive thresholds for non-caching providers (~50% of normal).
	aggressiveMaxInlineChars = 1000
	aggressiveMaxInlineBytes = 25 * 1024
	aggressiveMaxInlineLines = 1000
	aggressiveMaxLineLength  = 1000

	// distillHeadLines is how many lines to keep at the head of large outputs.
	distillHeadLines = 20
	// distillTailLines is how many lines to keep at the tail of large outputs.
	distillTailLines = 10

	// Aggressive head/tail for non-caching providers.
	aggressiveHeadLines = 12
	aggressiveTailLines = 6
)

// DistillConfig controls tool output distillation behavior.
type DistillConfig struct {
	// MaxInlineChars is the maximum characters to keep inline.
	// Outputs exceeding this are distilled and the full output saved to disk.
	MaxInlineChars int `json:"maxInlineChars,omitempty"`

	// MaxInlineBytes is the byte-level threshold. Outputs exceeding this
	// are distilled regardless of character count. Default: 50 KB.
	MaxInlineBytes int `json:"maxInlineBytes,omitempty"`

	// MaxInlineLines is the line-count threshold. Outputs exceeding this
	// are distilled regardless of size. Default: 2000.
	MaxInlineLines int `json:"maxInlineLines,omitempty"`

	// MaxLineLength caps individual line length. Lines exceeding this are
	// truncated with a suffix marker. Protects against minified files.
	// Default: 2000.
	MaxLineLength int `json:"maxLineLength,omitempty"`

	// FullOutputDir is where full outputs are saved.
	// Empty disables disk saving.
	FullOutputDir string `json:"fullOutputDir,omitempty"`

	// ExemptTools are tool names that bypass distillation.
	// Typically includes "read_file" whose output IS the point.
	ExemptTools []string `json:"exemptTools,omitempty"`

	// AggressiveMode enables tighter distillation thresholds for non-caching
	// providers (OpenAI, Moonshot/Kimi) where every input token is billed at
	// full price every turn. Thresholds are roughly halved.
	AggressiveMode bool `json:"aggressiveMode,omitempty"`
}

// DefaultDistillConfig returns sensible defaults.
func DefaultDistillConfig() DistillConfig {
	return DistillConfig{
		MaxInlineChars: DefaultMaxInlineChars,
		MaxInlineBytes: DefaultMaxInlineBytes,
		MaxInlineLines: DefaultMaxInlineLines,
		MaxLineLength:  DefaultMaxLineLength,
		ExemptTools:    []string{"read_file", "read_multiple_files"},
	}
}

// AggressiveDistillConfig returns tighter thresholds for non-caching providers.
func AggressiveDistillConfig() DistillConfig {
	return DistillConfig{
		MaxInlineChars: aggressiveMaxInlineChars,
		MaxInlineBytes: aggressiveMaxInlineBytes,
		MaxInlineLines: aggressiveMaxInlineLines,
		MaxLineLength:  aggressiveMaxLineLength,
		AggressiveMode: true,
		ExemptTools:    []string{"read_file", "read_multiple_files"},
	}
}

// DistillToolOutput distills a large tool output, keeping it concise inline
// and optionally saving the full output to disk for later re-reading.
//
// Distillation triggers when ANY of these thresholds is exceeded:
//   - MaxInlineChars (character count)
//   - MaxInlineBytes (byte count — catches multi-byte heavy content)
//   - MaxInlineLines (line count — catches verbose but short-line output)
func DistillToolOutput(toolName string, output string, cfg DistillConfig) string {
	applyDefaults(&cfg)

	// Check exemptions.
	if slices.Contains(cfg.ExemptTools, toolName) {
		return output
	}

	// Truncate individual long lines first (e.g., minified JS).
	output = truncateLongLines(output, cfg.MaxLineLength)

	// Check all thresholds — distill if any is exceeded.
	lines := strings.Split(output, "\n")
	totalLines := len(lines)
	needsDistill := len(output) > cfg.MaxInlineChars ||
		len(output) > cfg.MaxInlineBytes ||
		totalLines > cfg.MaxInlineLines

	if !needsDistill {
		return output
	}

	// Select head/tail line counts based on mode.
	headN, tailN := distillHeadLines, distillTailLines
	if cfg.AggressiveMode {
		headN, tailN = aggressiveHeadLines, aggressiveTailLines
	}

	// If few enough lines, just truncate by chars.
	if totalLines <= headN+tailN {
		return truncateChars(output, cfg.MaxInlineChars)
	}

	// Stage 1: Structural truncation — head + tail lines.
	head := lines[:headN]
	tail := lines[totalLines-tailN:]
	omitted := totalLines - headN - tailN

	// Stage 2: Save full output to disk if configured.
	var savedPath string
	if cfg.FullOutputDir != "" {
		savedPath = saveFullOutput(cfg.FullOutputDir, toolName, output)
	}

	// Build distilled output.
	var b strings.Builder
	for _, line := range head {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if savedPath != "" {
		fmt.Fprintf(&b, "\n[... %d lines omitted, full output saved to %s ...]\n", omitted, savedPath)
		b.WriteString("Use Grep to search the full content or Read with offset/limit to view specific sections.\n\n")
	} else {
		fmt.Fprintf(&b, "\n[... %d lines omitted ...]\n\n", omitted)
	}

	for _, line := range tail {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

// applyDefaults fills zero-value config fields with defaults.
func applyDefaults(cfg *DistillConfig) {
	if cfg.MaxInlineChars <= 0 {
		cfg.MaxInlineChars = DefaultMaxInlineChars
	}
	if cfg.MaxInlineBytes <= 0 {
		cfg.MaxInlineBytes = DefaultMaxInlineBytes
	}
	if cfg.MaxInlineLines <= 0 {
		cfg.MaxInlineLines = DefaultMaxInlineLines
	}
	if cfg.MaxLineLength <= 0 {
		cfg.MaxLineLength = DefaultMaxLineLength
	}
}

// truncateLongLines caps each line to maxLen characters, appending a marker.
func truncateLongLines(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	changed := false
	for i, line := range lines {
		if len(line) > maxLen {
			lines[i] = line[:maxLen] + "... (line truncated)"
			changed = true
		}
	}
	if !changed {
		return s
	}
	return strings.Join(lines, "\n")
}

// saveFullOutput writes the complete output to a file and returns the path.
func saveFullOutput(dir, toolName, output string) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}

	ts := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s.txt", toolName, ts)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return ""
	}
	return path
}

func truncateChars(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "\n[... truncated ...]"
}
