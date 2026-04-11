package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultMaxInlineChars is the default threshold for tool output distillation.
	DefaultMaxInlineChars = 2000
	// distillHeadLines is how many lines to keep at the head of large outputs.
	distillHeadLines = 20
	// distillTailLines is how many lines to keep at the tail of large outputs.
	distillTailLines = 10
)

// DistillConfig controls tool output distillation behavior.
type DistillConfig struct {
	// MaxInlineChars is the maximum characters to keep inline.
	// Outputs exceeding this are distilled and the full output saved to disk.
	MaxInlineChars int `json:"maxInlineChars,omitempty"`

	// FullOutputDir is where full outputs are saved.
	// Empty disables disk saving.
	FullOutputDir string `json:"fullOutputDir,omitempty"`

	// ExemptTools are tool names that bypass distillation.
	// Typically includes "read_file" whose output IS the point.
	ExemptTools []string `json:"exemptTools,omitempty"`
}

// DefaultDistillConfig returns sensible defaults.
func DefaultDistillConfig() DistillConfig {
	return DistillConfig{
		MaxInlineChars: DefaultMaxInlineChars,
		ExemptTools:    []string{"read_file", "read_multiple_files"},
	}
}

// DistillToolOutput distills a large tool output, keeping it concise inline
// and optionally saving the full output to disk for later re-reading.
func DistillToolOutput(toolName string, output string, cfg DistillConfig) string {
	if cfg.MaxInlineChars <= 0 {
		cfg.MaxInlineChars = DefaultMaxInlineChars
	}

	// Check exemptions.
	for _, exempt := range cfg.ExemptTools {
		if toolName == exempt {
			return output
		}
	}

	// Small enough — keep as-is.
	if len(output) <= cfg.MaxInlineChars {
		return output
	}

	// Split into lines for structural distillation.
	lines := strings.Split(output, "\n")
	totalLines := len(lines)

	// If few enough lines, just truncate by chars.
	if totalLines <= distillHeadLines+distillTailLines {
		return truncateChars(output, cfg.MaxInlineChars)
	}

	// Stage 1: Structural truncation — head + tail lines.
	head := lines[:distillHeadLines]
	tail := lines[totalLines-distillTailLines:]
	omitted := totalLines - distillHeadLines - distillTailLines

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
		b.WriteString(fmt.Sprintf("\n[... %d lines omitted, full output saved to %s ...]\n\n", omitted, savedPath))
	} else {
		b.WriteString(fmt.Sprintf("\n[... %d lines omitted ...]\n\n", omitted))
	}

	for _, line := range tail {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
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
