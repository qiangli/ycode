package agentsmd

import (
	"os"
	"regexp"
	"strings"
)

// targetLinePattern matches Makefile target declarations like "build:" or "test-all:".
var targetLinePattern = regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9_-]*):\s*`)

// ParseMakefileTargets reads a Makefile and returns the set of declared targets.
// It parses .PHONY lines and target: declarations.
func ParseMakefileTargets(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	targets := make(map[string]bool)
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Parse .PHONY declarations.
		if strings.HasPrefix(trimmed, ".PHONY:") {
			rest := strings.TrimPrefix(trimmed, ".PHONY:")
			for _, t := range strings.Fields(rest) {
				targets[t] = true
			}
			continue
		}

		// Parse target: lines (skip variables, comments, recipes).
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, ".") || trimmed == "" {
			continue
		}

		// Skip variable assignments.
		if strings.Contains(trimmed, "=") && !strings.Contains(trimmed, ":=") {
			if idx := strings.Index(trimmed, "="); idx < strings.Index(trimmed+":", ":") {
				continue
			}
		}

		if m := targetLinePattern.FindStringSubmatch(trimmed); m != nil {
			targets[m[1]] = true
		}
	}

	return targets, nil
}
