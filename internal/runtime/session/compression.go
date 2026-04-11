package session

import (
	"fmt"
	"strings"
)

const (
	// MaxSummaryChars is the maximum character budget for a summary.
	MaxSummaryChars = 1200
	// MaxSummaryLines is the maximum line count for a summary.
	MaxSummaryLines = 24
	// MaxLineChars is the maximum characters per line.
	MaxLineChars = 160
)

// CompressSummary enforces the summary budget using priority-based line selection.
func CompressSummary(summary string) string {
	return CompressSummaryWithBudget(summary, MaxSummaryChars, MaxSummaryLines, MaxLineChars)
}

// CompressSummaryWithBudget enforces a custom summary budget.
func CompressSummaryWithBudget(summary string, maxChars, maxLines, maxLineChars int) string {
	// Normalize lines: collapse whitespace, deduplicate, truncate.
	normalized := normalizeLines(summary, maxLineChars)
	if len(normalized.lines) == 0 || maxChars == 0 || maxLines == 0 {
		return ""
	}

	// Priority-based line selection.
	selected := selectLineIndexes(normalized.lines, maxChars, maxLines)

	var compressed []string
	for _, idx := range selected {
		compressed = append(compressed, normalized.lines[idx])
	}

	// Ensure at least one line.
	if len(compressed) == 0 && len(normalized.lines) > 0 {
		line := normalized.lines[0]
		if len([]rune(line)) > maxChars {
			line = string([]rune(line)[:maxChars-1]) + "…"
		}
		compressed = append(compressed, line)
	}

	// Add omission notice if lines were dropped.
	omitted := len(normalized.lines) - len(compressed)
	if omitted > 0 {
		notice := fmt.Sprintf("- … %d additional line(s) omitted.", omitted)
		compressed = pushLineWithBudget(compressed, notice, maxChars, maxLines)
	}

	return strings.Join(compressed, "\n")
}

type normalizedSummary struct {
	lines                 []string
	removedDuplicateLines int
}

func normalizeLines(summary string, maxLineChars int) normalizedSummary {
	seen := make(map[string]bool)
	var lines []string
	removedDups := 0

	for _, rawLine := range strings.Split(summary, "\n") {
		normalized := collapseInlineWhitespace(rawLine)
		if normalized == "" {
			continue
		}

		truncated := truncateLine(normalized, maxLineChars)
		dedupeKey := strings.ToLower(truncated)
		if seen[dedupeKey] {
			removedDups++
			continue
		}
		seen[dedupeKey] = true
		lines = append(lines, truncated)
	}

	return normalizedSummary{lines: lines, removedDuplicateLines: removedDups}
}

// selectLineIndexes selects lines by priority order within budget constraints.
func selectLineIndexes(lines []string, maxChars, maxLines int) []int {
	selected := make(map[int]bool)

	for priority := 0; priority <= 3; priority++ {
		for i, line := range lines {
			if selected[i] || linePriority(line) != priority {
				continue
			}

			// Check if adding this line stays within budget.
			candidateCount := len(selected) + 1
			if candidateCount > maxLines {
				continue
			}

			candidateChars := joinedCharCount(lines, selected) + len([]rune(line))
			if candidateCount > 1 {
				candidateChars++ // newline separator
			}
			if candidateChars > maxChars {
				continue
			}

			selected[i] = true
		}
	}

	// Return sorted indexes.
	var result []int
	for i := range lines {
		if selected[i] {
			result = append(result, i)
		}
	}
	return result
}

func joinedCharCount(lines []string, selected map[int]bool) int {
	total := 0
	count := 0
	for i, line := range lines {
		if selected[i] {
			total += len([]rune(line))
			count++
		}
	}
	if count > 1 {
		total += count - 1 // newline separators
	}
	return total
}

// linePriority assigns a selection priority (lower = more important).
func linePriority(line string) int {
	if line == "Summary:" || line == "Conversation summary:" || isCoreDetail(line) {
		return 0
	}
	if isSectionHeader(line) {
		return 1
	}
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "  - ") {
		return 2
	}
	return 3
}

func isCoreDetail(line string) bool {
	prefixes := []string{
		// Legacy format.
		"- Scope:",
		"- Current work:",
		"- Pending work:",
		"- Key files referenced:",
		"- Tools mentioned:",
		"- Recent user requests:",
		"- Previously compacted context:",
		"- Newly compacted context:",
		// Intent summary format.
		"Scope:",
		"Primary Goal:",
		"Verified Facts:",
		"Working Set:",
		"Active Blockers:",
		"Decision Log:",
		"Key Files:",
		"Tools Used:",
		"Pending Work:",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func isSectionHeader(line string) bool {
	return strings.HasSuffix(line, ":")
}

func collapseInlineWhitespace(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

func truncateLine(line string, maxChars int) string {
	runes := []rune(line)
	if maxChars == 0 || len(runes) <= maxChars {
		return line
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}

func pushLineWithBudget(lines []string, line string, maxChars, maxLines int) []string {
	candidateCount := len(lines) + 1
	if candidateCount > maxLines {
		return lines
	}

	totalChars := 0
	for _, l := range lines {
		totalChars += len([]rune(l))
	}
	totalChars += len([]rune(line))
	totalChars += candidateCount - 1 // newline separators

	if totalChars > maxChars {
		return lines
	}

	return append(lines, line)
}
