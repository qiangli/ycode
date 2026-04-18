package prompt

import (
	"strings"
)

const (
	// MaxRefreshBudget is the maximum characters for post-compaction context refresh.
	MaxRefreshBudget = 1500
)

// PostCompactionRefresh extracts critical sections from instruction files
// that should be re-injected after compaction so the agent doesn't lose
// project-specific rules.
//
// This follows OpenClaw's pattern of re-reading key AGENTS.md sections
// after compaction. Default sections: "Build & Test", "Key Design Decisions".
func PostCompactionRefresh(contextFiles []ContextFile) string {
	if len(contextFiles) == 0 {
		return ""
	}

	// Extract key sections from the first (most relevant) CLAUDE.md.
	// These are sections that contain critical operational instructions.
	keySections := []string{
		"Build & Test",
		"Key Design Decisions",
		"Dependencies",
	}

	var parts []string
	totalChars := 0

	for _, cf := range contextFiles {
		for _, section := range keySections {
			content := extractMarkdownSection(cf.Content, section)
			if content == "" {
				continue
			}

			// Budget check.
			if totalChars+len(content) > MaxRefreshBudget {
				remaining := MaxRefreshBudget - totalChars
				if remaining > 100 {
					content = content[:remaining-20] + "\n... (truncated)"
				} else {
					continue
				}
			}

			parts = append(parts, "## "+section+"\n"+content)
			totalChars += len(content)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "# Critical project context (re-injected after compaction)\n\n" +
		strings.Join(parts, "\n\n")
}

// extractMarkdownSection extracts the content of a markdown section by heading.
// It looks for "## heading" or "### heading" and returns content until the next
// heading of equal or higher level.
func extractMarkdownSection(content, heading string) string {
	lines := strings.Split(content, "\n")
	headingLower := strings.ToLower(heading)

	var result []string
	inSection := false
	sectionLevel := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a heading line.
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, ch := range trimmed {
				if ch == '#' {
					level++
				} else {
					break
				}
			}

			headingText := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))

			if inSection {
				// End section if we hit a heading of equal or higher level.
				if level <= sectionLevel {
					break
				}
			}

			if strings.ToLower(headingText) == headingLower {
				inSection = true
				sectionLevel = level
				continue
			}
		}

		if inSection {
			result = append(result, line)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}
