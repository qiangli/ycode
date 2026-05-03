package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LintConfig configures the lint runner.
type LintConfig struct {
	Enabled    bool
	MaxRetries int               // max lint-fix cycles (default 2)
	Commands   map[string]string // extension → command template (use %s for file path)
}

// DefaultLintConfig returns a config with Go lint enabled.
func DefaultLintConfig() LintConfig {
	return LintConfig{
		Enabled:    false,
		MaxRetries: 2,
		Commands: map[string]string{
			".go": "go vet %s",
		},
	}
}

// LintError represents a single lint finding.
type LintError struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
	Rule    string `json:"rule,omitempty"`
}

// LintRunner runs lint commands and parses their output.
type LintRunner struct {
	config LintConfig
}

// NewLintRunner creates a lint runner with the given config.
func NewLintRunner(config LintConfig) *LintRunner {
	return &LintRunner{config: config}
}

// RunOnFile runs the configured lint command for the given file's extension.
// Returns lint errors, raw output, and any execution error.
func (r *LintRunner) RunOnFile(path string) ([]LintError, string, error) {
	ext := filepath.Ext(path)
	cmdTemplate, ok := r.config.Commands[ext]
	if !ok {
		return nil, "", nil // no lint command for this extension
	}

	cmd := fmt.Sprintf(cmdTemplate, path)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil, "", fmt.Errorf("empty lint command")
	}

	out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
	output := string(out)

	if err == nil && output == "" {
		return nil, "", nil // no errors
	}

	errors := ParseLintOutput(output, ext)
	return errors, output, nil
}

// ParseLintOutput parses lint output into structured errors.
// Supports the common "file:line:col: message" format used by Go, gcc, etc.
func ParseLintOutput(output, ext string) []LintError {
	var errors []LintError

	// Pattern: file:line:col: message
	re := regexp.MustCompile(`^([^:]+):(\d+):(\d+):\s*(.+)$`)
	// Fallback pattern: file:line: message (no column)
	reFallback := regexp.MustCompile(`^([^:]+):(\d+):\s*(.+)$`)

	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if m := re.FindStringSubmatch(line); m != nil {
			lineNum, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			errors = append(errors, LintError{
				File:    m[1],
				Line:    lineNum,
				Column:  col,
				Message: m[4],
			})
		} else if m := reFallback.FindStringSubmatch(line); m != nil {
			lineNum, _ := strconv.Atoi(m[2])
			errors = append(errors, LintError{
				File:    m[1],
				Line:    lineNum,
				Message: m[3],
			})
		}
	}

	return errors
}

// FormatLintFeedback creates a human-readable feedback string with code context.
func FormatLintFeedback(errors []LintError, filePath string) string {
	if len(errors) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\nLINT ERRORS (fix before proceeding):\n")

	// Try to read file for context.
	var fileLines []string
	data, err := os.ReadFile(filePath)
	if err == nil {
		fileLines = strings.Split(string(data), "\n")
	}

	for _, e := range errors {
		fmt.Fprintf(&b, "  %s:%d:%d: %s\n", e.File, e.Line, e.Column, e.Message)

		// Add code context if file is available.
		if len(fileLines) > 0 && e.Line > 0 && e.Line <= len(fileLines) {
			startLine := e.Line - 3
			if startLine < 1 {
				startLine = 1
			}
			endLine := e.Line + 2
			if endLine > len(fileLines) {
				endLine = len(fileLines)
			}

			for i := startLine; i <= endLine; i++ {
				prefix := "    "
				if i == e.Line {
					prefix = "  > "
				}
				fmt.Fprintf(&b, "%s%4d | %s\n", prefix, i, fileLines[i-1])
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}
