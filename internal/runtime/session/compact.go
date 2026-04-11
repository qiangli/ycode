package session

import (
	"fmt"
	"strings"
)

const (
	// CompactionThreshold in tokens to trigger compaction.
	CompactionThreshold = 100_000
	// PreserveLastMessages is the number of recent messages to keep verbatim.
	PreserveLastMessages = 4

	compactContinuationPreamble = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n"
	compactRecentMessagesNote   = "Recent messages are preserved verbatim."
	compactDirectResumeInstr    = "Continue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, and do not preface with continuation text."
)

// CompactionResult holds the outcome of a compaction.
type CompactionResult struct {
	Summary          string
	FormattedSummary string
	PreservedCount   int
	CompactedCount   int
	PreviousSummary  string
}

// NeedsCompaction checks if the session exceeds the token threshold.
func NeedsCompaction(estimatedTokens int) bool {
	return estimatedTokens > CompactionThreshold
}

// EstimateMessageTokens roughly estimates the token footprint of a message.
func EstimateMessageTokens(msg ConversationMessage) int {
	total := 0
	for _, block := range msg.Content {
		switch block.Type {
		case ContentTypeText:
			total += len(block.Text)/4 + 1
		case ContentTypeToolUse:
			total += (len(block.Name)+len(block.Input))/4 + 1
		case ContentTypeToolResult:
			total += (len(block.Name)+len(block.Content))/4 + 1
		}
	}
	return total
}

// Compact produces a summary from older messages, keeping recent ones.
// It ensures tool-use/tool-result pairs are not split at the compaction boundary.
func Compact(messages []ConversationMessage, previousSummary string) *CompactionResult {
	if len(messages) <= PreserveLastMessages {
		return nil
	}

	// Determine compaction boundary.
	rawKeepFrom := len(messages) - PreserveLastMessages

	// Check if there's an existing compacted summary prefix.
	compactedPrefixLen := 0
	if len(messages) > 0 && extractExistingCompactedSummary(messages[0]) != "" {
		compactedPrefixLen = 1
	}

	// Ensure we do not split a tool-use / tool-result pair at the boundary.
	// If the first preserved message starts with a ToolResult, walk the boundary
	// back to include the matching assistant ToolUse message.
	keepFrom := rawKeepFrom
	for keepFrom > compactedPrefixLen {
		firstPreserved := messages[keepFrom]
		startsWithToolResult := len(firstPreserved.Content) > 0 &&
			firstPreserved.Content[0].Type == ContentTypeToolResult

		if !startsWithToolResult {
			break
		}

		// Check the preceding message for ToolUse.
		if keepFrom > 0 {
			preceding := messages[keepFrom-1]
			hasToolUse := false
			for _, b := range preceding.Content {
				if b.Type == ContentTypeToolUse {
					hasToolUse = true
					break
				}
			}
			if hasToolUse {
				// Pair is intact — include the assistant turn too.
				keepFrom--
				break
			}
		}

		// Orphaned tool result — walk back further.
		keepFrom--
	}

	toCompact := messages[compactedPrefixLen:keepFrom]
	summary := summarizeMessages(toCompact)

	// Merge with previous summary if present.
	if previousSummary != "" {
		summary = mergeCompactSummaries(previousSummary, summary)
	} else if existingSummary := extractExistingCompactedSummary(messages[0]); existingSummary != "" {
		summary = mergeCompactSummaries(existingSummary, summary)
	}

	formatted := formatCompactSummary(summary)

	return &CompactionResult{
		Summary:          summary,
		FormattedSummary: formatted,
		PreservedCount:   len(messages) - keepFrom,
		CompactedCount:   len(toCompact),
		PreviousSummary:  previousSummary,
	}
}

// GetCompactContinuationMessage builds the synthetic system message used after compaction.
func GetCompactContinuationMessage(summary string, suppressFollowUp, recentPreserved bool) string {
	base := compactContinuationPreamble + formatCompactSummary(summary)

	if recentPreserved {
		base += "\n\n" + compactRecentMessagesNote
	}
	if suppressFollowUp {
		base += "\n" + compactDirectResumeInstr
	}

	return base
}

// formatCompactSummary normalizes a compaction summary into user-facing text.
func formatCompactSummary(summary string) string {
	// Strip <analysis> blocks.
	without := stripTagBlock(summary, "analysis")

	// Replace <summary>...</summary> with "Summary:\n..."
	if content := extractTagBlock(without, "summary"); content != "" {
		without = strings.Replace(without,
			fmt.Sprintf("<summary>%s</summary>", content),
			fmt.Sprintf("Summary:\n%s", strings.TrimSpace(content)),
			1)
	}

	return strings.TrimSpace(collapseBlankLines(without))
}

// summarizeMessages produces a structured summary of compacted messages.
func summarizeMessages(messages []ConversationMessage) string {
	userCount, assistantCount, toolCount := 0, 0, 0
	for _, m := range messages {
		switch m.Role {
		case RoleUser:
			userCount++
		case RoleAssistant:
			assistantCount++
		}
		for _, c := range m.Content {
			if c.Type == ContentTypeToolUse || c.Type == ContentTypeToolResult {
				toolCount++
			}
		}
	}

	var lines []string
	lines = append(lines, "<summary>")
	lines = append(lines, "Conversation summary:")
	lines = append(lines, fmt.Sprintf("- Scope: %d earlier messages compacted (user=%d, assistant=%d, tool=%d).",
		len(messages), userCount, assistantCount, toolCount))

	// Tools mentioned.
	toolSet := make(map[string]bool)
	for _, m := range messages {
		for _, c := range m.Content {
			switch c.Type {
			case ContentTypeToolUse:
				if c.Name != "" {
					toolSet[c.Name] = true
				}
			case ContentTypeToolResult:
				if c.Name != "" {
					toolSet[c.Name] = true
				}
			}
		}
	}
	if len(toolSet) > 0 {
		var names []string
		for name := range toolSet {
			names = append(names, name)
		}
		lines = append(lines, fmt.Sprintf("- Tools mentioned: %s.", strings.Join(names, ", ")))
	}

	// Recent user requests (last 3).
	recentRequests := collectRecentRoleSummaries(messages, RoleUser, 3)
	if len(recentRequests) > 0 {
		lines = append(lines, "- Recent user requests:")
		for _, req := range recentRequests {
			lines = append(lines, "  - "+req)
		}
	}

	// Pending work (inferred from keywords).
	pendingWork := inferPendingWork(messages)
	if len(pendingWork) > 0 {
		lines = append(lines, "- Pending work:")
		for _, item := range pendingWork {
			lines = append(lines, "  - "+item)
		}
	}

	// Key files referenced.
	keyFiles := collectKeyFiles(messages)
	if len(keyFiles) > 0 {
		lines = append(lines, fmt.Sprintf("- Key files referenced: %s.", strings.Join(keyFiles, ", ")))
	}

	// Current work.
	if currentWork := inferCurrentWork(messages); currentWork != "" {
		lines = append(lines, fmt.Sprintf("- Current work: %s", currentWork))
	}

	// Key timeline.
	lines = append(lines, "- Key timeline:")
	for _, m := range messages {
		role := string(m.Role)
		var blockSummaries []string
		for _, b := range m.Content {
			blockSummaries = append(blockSummaries, summarizeBlock(b))
		}
		content := strings.Join(blockSummaries, " | ")
		lines = append(lines, fmt.Sprintf("  - %s: %s", role, content))
	}

	lines = append(lines, "</summary>")
	return strings.Join(lines, "\n")
}

// summarizeBlock returns a truncated summary of a content block.
func summarizeBlock(block ContentBlock) string {
	var raw string
	switch block.Type {
	case ContentTypeText:
		raw = block.Text
	case ContentTypeToolUse:
		raw = fmt.Sprintf("tool_use %s(%s)", block.Name, string(block.Input))
	case ContentTypeToolResult:
		prefix := ""
		if block.IsError {
			prefix = "error "
		}
		raw = fmt.Sprintf("tool_result %s: %s%s", block.Name, prefix, block.Content)
	}
	return truncateSummary(raw, 160)
}

// collectRecentRoleSummaries collects the last N text summaries from messages of a given role.
func collectRecentRoleSummaries(messages []ConversationMessage, role MessageRole, limit int) []string {
	var results []string
	for i := len(messages) - 1; i >= 0 && len(results) < limit; i-- {
		if messages[i].Role != role {
			continue
		}
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}
		results = append(results, truncateSummary(text, 160))
	}
	// Reverse to chronological order.
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results
}

// inferPendingWork extracts messages containing work-in-progress keywords.
func inferPendingWork(messages []ConversationMessage) []string {
	var results []string
	for i := len(messages) - 1; i >= 0 && len(results) < 3; i-- {
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}
		lowered := strings.ToLower(text)
		if strings.Contains(lowered, "todo") ||
			strings.Contains(lowered, "next") ||
			strings.Contains(lowered, "pending") ||
			strings.Contains(lowered, "follow up") ||
			strings.Contains(lowered, "remaining") {
			results = append(results, truncateSummary(text, 160))
		}
	}
	// Reverse to chronological order.
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results
}

// collectKeyFiles extracts file path candidates from message content.
func collectKeyFiles(messages []ConversationMessage) []string {
	fileSet := make(map[string]bool)
	var files []string

	for _, m := range messages {
		for _, c := range m.Content {
			// Extract from all content fields.
			for _, text := range []string{c.Text, string(c.Input), c.Content} {
				for _, candidate := range extractFileCandidates(text) {
					if !fileSet[candidate] && len(files) < 8 {
						fileSet[candidate] = true
						files = append(files, candidate)
					}
				}
			}
		}
	}
	return files
}

// extractFileCandidates finds file-path-like tokens with interesting extensions.
func extractFileCandidates(text string) []string {
	var candidates []string
	for _, word := range strings.Fields(text) {
		cleaned := strings.Trim(word, "\"'`,;:()")
		if strings.Contains(cleaned, "/") && hasInterestingExtension(cleaned) && len(cleaned) < 200 {
			candidates = append(candidates, cleaned)
		}
	}
	return candidates
}

// hasInterestingExtension checks if a path has a common source file extension.
func hasInterestingExtension(path string) bool {
	extensions := []string{".go", ".rs", ".ts", ".tsx", ".js", ".json", ".md", ".py", ".yaml", ".yml", ".toml"}
	for _, ext := range extensions {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// inferCurrentWork finds the most recent non-empty text block.
func inferCurrentWork(messages []ConversationMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		text := firstTextBlock(messages[i])
		if text != "" {
			return truncateSummary(text, 200)
		}
	}
	return ""
}

// firstTextBlock extracts the first non-empty text block from a message.
func firstTextBlock(msg ConversationMessage) string {
	for _, c := range msg.Content {
		if c.Type == ContentTypeText && strings.TrimSpace(c.Text) != "" {
			return c.Text
		}
	}
	return ""
}

// mergeCompactSummaries merges a previous summary with a new one.
func mergeCompactSummaries(existingSummary, newSummary string) string {
	previousHighlights := extractSummaryHighlights(existingSummary)
	newFormatted := formatCompactSummary(newSummary)
	newHighlights := extractSummaryHighlights(newFormatted)
	newTimeline := extractSummaryTimeline(newFormatted)

	var lines []string
	lines = append(lines, "<summary>")
	lines = append(lines, "Conversation summary:")

	if len(previousHighlights) > 0 {
		lines = append(lines, "- Previously compacted context:")
		for _, line := range previousHighlights {
			lines = append(lines, "  "+line)
		}
	}

	if len(newHighlights) > 0 {
		lines = append(lines, "- Newly compacted context:")
		for _, line := range newHighlights {
			lines = append(lines, "  "+line)
		}
	}

	if len(newTimeline) > 0 {
		lines = append(lines, "- Key timeline:")
		for _, line := range newTimeline {
			lines = append(lines, "  "+line)
		}
	}

	lines = append(lines, "</summary>")
	return strings.Join(lines, "\n")
}

// extractSummaryHighlights extracts non-timeline lines from a formatted summary.
func extractSummaryHighlights(summary string) []string {
	var lines []string
	inTimeline := false

	for _, line := range strings.Split(formatCompactSummary(summary), "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "" || trimmed == "Summary:" || trimmed == "Conversation summary:" {
			continue
		}
		if trimmed == "- Key timeline:" {
			inTimeline = true
			continue
		}
		if inTimeline {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// extractSummaryTimeline extracts timeline lines from a formatted summary.
func extractSummaryTimeline(summary string) []string {
	var lines []string
	inTimeline := false

	for _, line := range strings.Split(formatCompactSummary(summary), "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "- Key timeline:" {
			inTimeline = true
			continue
		}
		if !inTimeline {
			continue
		}
		if trimmed == "" {
			break
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// truncateSummary truncates text to max chars with ellipsis.
func truncateSummary(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "…"
}

// extractExistingCompactedSummary checks if a message is a compaction summary.
func extractExistingCompactedSummary(msg ConversationMessage) string {
	if msg.Role != RoleSystem {
		return ""
	}
	text := firstTextBlock(msg)
	if text == "" {
		return ""
	}
	rest, found := strings.CutPrefix(text, compactContinuationPreamble)
	if !found {
		return ""
	}
	// Strip trailing notes.
	if idx := strings.Index(rest, "\n\n"+compactRecentMessagesNote); idx >= 0 {
		rest = rest[:idx]
	}
	if idx := strings.Index(rest, "\n"+compactDirectResumeInstr); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.TrimSpace(rest)
}

// extractTagBlock extracts content between <tag>...</tag>.
func extractTagBlock(content, tag string) string {
	start := fmt.Sprintf("<%s>", tag)
	end := fmt.Sprintf("</%s>", tag)
	startIdx := strings.Index(content, start)
	if startIdx < 0 {
		return ""
	}
	startIdx += len(start)
	endIdx := strings.Index(content[startIdx:], end)
	if endIdx < 0 {
		return ""
	}
	return content[startIdx : startIdx+endIdx]
}

// stripTagBlock removes a <tag>...</tag> block from content.
func stripTagBlock(content, tag string) string {
	start := fmt.Sprintf("<%s>", tag)
	end := fmt.Sprintf("</%s>", tag)
	startIdx := strings.Index(content, start)
	endIdx := strings.Index(content, end)
	if startIdx < 0 || endIdx < 0 {
		return content
	}
	return content[:startIdx] + content[endIdx+len(end):]
}

// collapseBlankLines collapses consecutive blank lines into one.
func collapseBlankLines(content string) string {
	var result strings.Builder
	lastBlank := false
	for _, line := range strings.Split(content, "\n") {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && lastBlank {
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
		lastBlank = isBlank
	}
	return result.String()
}
