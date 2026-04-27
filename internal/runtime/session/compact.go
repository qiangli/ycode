package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

const (
	// CompactionThreshold in tokens to trigger compaction.
	CompactionThreshold = 100_000
	// PreserveLastMessages is the number of recent messages to keep verbatim.
	PreserveLastMessages = 4

	// HeadProtectedTurns is the number of initial user turns to always preserve.
	// These provide critical context-setting information.
	HeadProtectedTurns = 2

	// TailProtectedTokens is the token budget reserved for recent messages.
	// The tail gets exact message preservation, not summarization.
	TailProtectedTokens = 8000

	compactContinuationPreamble = "A previous model instance worked on this task and produced a context checkpoint handoff. Use it to build on completed work, avoid duplicating effort, and verify current state with tools before making assumptions.\n\n"
	compactRecentMessagesNote   = "Recent messages are preserved verbatim."
	compactDirectResumeInstr    = "Continue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, and do not preface with continuation text."
)

// CompactionResult holds the outcome of a compaction.
type CompactionResult struct {
	Summary            string
	FormattedSummary   string
	PreservedCount     int
	CompactedCount     int
	PreviousSummary    string
	HeadPreservedCount int
}

// NeedsCompaction checks if the session exceeds the token threshold.
func NeedsCompaction(estimatedTokens int) bool {
	return estimatedTokens > CompactionThreshold
}

// EstimateMessageTokens roughly estimates the token footprint of a message.
// Uses CJK-aware estimation: ASCII chars ≈ 0.25 tokens, non-ASCII ≈ 1.3 tokens.
func EstimateMessageTokens(msg ConversationMessage) int {
	total := 0
	for _, block := range msg.Content {
		switch block.Type {
		case ContentTypeText:
			total += EstimateTextTokens(block.Text)
		case ContentTypeToolUse:
			total += EstimateTextTokens(block.Name) + EstimateTextTokens(string(block.Input))
		case ContentTypeToolResult:
			total += EstimateTextTokens(block.Name) + EstimateTextTokens(block.Content)
		}
	}
	return total
}

// maxCharsForFullHeuristic is the threshold above which we use the fast len/4
// approximation instead of per-character scanning.
const maxCharsForFullHeuristic = 100_000

// EstimateTextTokens estimates token count for a text string with CJK awareness.
// ASCII characters (0-127) ≈ 0.25 tokens/char; non-ASCII (CJK, etc.) ≈ 1.3 tokens/char.
// For very large strings (>100K chars), falls back to len/4 for performance.
// Inspired by Gemini CLI's tokenCalculation.ts.
func EstimateTextTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	if len(text) > maxCharsForFullHeuristic {
		return len(text)/4 + 1
	}
	tokens := 0.0
	for _, r := range text {
		if r <= 127 {
			tokens += 0.25
		} else {
			tokens += 1.3
		}
	}
	return int(tokens) + 1
}

// Compact produces a summary from older messages, keeping recent ones.
// It ensures tool-use/tool-result pairs are not split at the compaction boundary.
// When maxHistoryTokens is provided and > 0, the summary is capped to that budget.
func Compact(messages []ConversationMessage, previousSummary string, maxHistoryTokens ...int) *CompactionResult {
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

	// Head protection: find the end of the first HeadProtectedTurns user messages.
	headProtectEnd := findHeadProtectEnd(messages, compactedPrefixLen, keepFrom)

	// Only summarize messages between headProtectEnd and keepFrom.
	// Head messages (compactedPrefixLen to headProtectEnd) are preserved verbatim.
	toCompact := messages[headProtectEnd:keepFrom]
	summary := buildIntentSummary(toCompact)

	// Merge with previous summary if present.
	if previousSummary != "" {
		summary = mergeCompactSummaries(previousSummary, summary)
	} else if existingSummary := extractExistingCompactedSummary(messages[0]); existingSummary != "" {
		summary = mergeCompactSummaries(existingSummary, summary)
	}

	// Extract and preserve structured task state across compaction.
	taskState := extractTaskState(toCompact)
	if taskState != "" {
		slog.Info("session.compact.task_state_preserved",
			"state_length", len(taskState),
		)
		summary += "\n\nTask State:\n" + taskState
	}

	// Enforce history budget cap if specified.
	if len(maxHistoryTokens) > 0 && maxHistoryTokens[0] > 0 {
		summary = EnforceSummaryCap(summary, maxHistoryTokens[0])
	}

	formatted := formatCompactSummary(summary)

	return &CompactionResult{
		Summary:            summary,
		FormattedSummary:   formatted,
		PreservedCount:     len(messages) - keepFrom,
		CompactedCount:     len(toCompact),
		PreviousSummary:    previousSummary,
		HeadPreservedCount: headProtectEnd - compactedPrefixLen,
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

	// Handle new <intent_summary> format.
	if content := extractTagBlock(without, "intent_summary"); content != "" {
		without = strings.Replace(without,
			fmt.Sprintf("<intent_summary>%s</intent_summary>", content),
			fmt.Sprintf("Summary:\n%s", strings.TrimSpace(content)),
			1)
	}

	// Handle legacy <summary> format for backward compatibility.
	if content := extractTagBlock(without, "summary"); content != "" {
		without = strings.Replace(without,
			fmt.Sprintf("<summary>%s</summary>", content),
			fmt.Sprintf("Summary:\n%s", strings.TrimSpace(content)),
			1)
	}

	return strings.TrimSpace(collapseBlankLines(without))
}

// buildIntentSummary produces a structured intent summary of compacted messages.
// The summary uses explicit categories to preserve key information across compaction.
func buildIntentSummary(messages []ConversationMessage) string {
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
	lines = append(lines, "<intent_summary>")
	lines = append(lines, fmt.Sprintf("Scope: %d messages compacted (user=%d, assistant=%d, tool=%d).",
		len(messages), userCount, assistantCount, toolCount))

	// Primary Goal: infer from recent user requests.
	if goal := inferPrimaryGoal(messages); goal != "" {
		lines = append(lines, "Primary Goal: "+goal)
	}

	// Verified Facts: successful tool outcomes.
	facts := extractVerifiedFacts(messages)
	if len(facts) > 0 {
		lines = append(lines, "Verified Facts:")
		for _, f := range facts {
			lines = append(lines, "- "+f)
		}
	}

	// Working Set: files actively being edited/written (not just read).
	workingSet := extractWorkingSet(messages)
	if len(workingSet) > 0 {
		lines = append(lines, "Working Set: "+strings.Join(workingSet, ", "))
	}

	// Active Blockers: errors, failures.
	blockers := extractActiveBlockers(messages)
	if len(blockers) > 0 {
		lines = append(lines, "Active Blockers:")
		for _, b := range blockers {
			lines = append(lines, "- "+b)
		}
	}

	// Decision Log: explicit choices made.
	decisions := extractDecisionLog(messages)
	if len(decisions) > 0 {
		lines = append(lines, "Decision Log:")
		for _, d := range decisions {
			lines = append(lines, "- "+d)
		}
	}

	// Key files referenced (broader than working set).
	keyFiles := collectKeyFiles(messages)
	if len(keyFiles) > 0 {
		lines = append(lines, "Key Files: "+strings.Join(keyFiles, ", "))
	}

	// Tools used.
	toolSet := make(map[string]bool)
	for _, m := range messages {
		for _, c := range m.Content {
			if (c.Type == ContentTypeToolUse || c.Type == ContentTypeToolResult) && c.Name != "" {
				toolSet[c.Name] = true
			}
		}
	}
	if len(toolSet) > 0 {
		var names []string
		for name := range toolSet {
			names = append(names, name)
		}
		lines = append(lines, "Tools Used: "+strings.Join(names, ", "))
	}

	// Pending work.
	pendingWork := inferPendingWork(messages)
	if len(pendingWork) > 0 {
		lines = append(lines, "Pending Work:")
		for _, item := range pendingWork {
			lines = append(lines, "- "+item)
		}
	}

	// Resolved Questions: question/answer pairs from the conversation.
	resolvedQs := extractResolvedQuestions(messages)
	if len(resolvedQs) > 0 {
		lines = append(lines, "Resolved Questions:")
		for _, q := range resolvedQs {
			lines = append(lines, "- "+q)
		}
	}

	// Active Task: the most recent unfinished user request.
	if activeTask := extractActiveTask(messages); activeTask != "" {
		lines = append(lines, "Active Task: "+activeTask)
	}

	// Remaining Work: pending items not yet addressed (distinct from Pending Work
	// which uses keyword heuristics — this looks at unanswered user requests).
	remaining := inferRemainingWork(messages)
	if len(remaining) > 0 {
		lines = append(lines, "Remaining Work:")
		for _, item := range remaining {
			lines = append(lines, "- "+item)
		}
	}

	lines = append(lines, "</intent_summary>")
	return strings.Join(lines, "\n")
}

// inferPrimaryGoal extracts the most likely top-level task from recent user messages.
func inferPrimaryGoal(messages []ConversationMessage) string {
	reqs := collectRecentRoleSummaries(messages, RoleUser, 3)
	if len(reqs) == 0 {
		return ""
	}
	// The most recent user request is typically the primary goal.
	return reqs[len(reqs)-1]
}

// extractVerifiedFacts scans tool results for successful outcomes.
func extractVerifiedFacts(messages []ConversationMessage) []string {
	var facts []string
	seen := make(map[string]bool)

	for i := len(messages) - 1; i >= 0 && len(facts) < 5; i-- {
		for _, c := range messages[i].Content {
			if c.Type != ContentTypeToolResult || c.IsError {
				continue
			}
			content := strings.ToLower(c.Content)
			var fact string
			switch {
			case strings.Contains(content, "pass") && (strings.Contains(content, "test") || strings.Contains(content, "ok ")):
				fact = "Tests passing: " + truncateSummary(c.Content, 100)
			case strings.Contains(content, "success") || strings.Contains(content, "build succeeded"):
				fact = "Build succeeded: " + truncateSummary(c.Content, 100)
			case c.Name == "write_file" || c.Name == "edit_file":
				fact = "File modified: " + truncateSummary(c.Content, 100)
			default:
				continue
			}
			if !seen[fact] {
				seen[fact] = true
				facts = append(facts, fact)
			}
		}
	}
	return facts
}

// extractWorkingSet identifies files that were written or edited (not just read).
func extractWorkingSet(messages []ConversationMessage) []string {
	fileSet := make(map[string]bool)
	var files []string

	writeTools := map[string]bool{
		"write_file": true, "edit_file": true,
	}

	for _, m := range messages {
		for _, c := range m.Content {
			if c.Type != ContentTypeToolUse || !writeTools[c.Name] {
				continue
			}
			// Extract path from tool input.
			for _, candidate := range extractFileCandidates(string(c.Input)) {
				if !fileSet[candidate] && len(files) < 10 {
					fileSet[candidate] = true
					files = append(files, candidate)
				}
			}
		}
	}
	return files
}

// extractActiveBlockers finds error outputs from recent tool executions.
func extractActiveBlockers(messages []ConversationMessage) []string {
	var blockers []string
	seen := make(map[string]bool)

	// Scan from newest to oldest, only look at recent messages.
	for i := len(messages) - 1; i >= 0 && len(blockers) < 3; i-- {
		for _, c := range messages[i].Content {
			if c.Type != ContentTypeToolResult || !c.IsError {
				continue
			}
			summary := truncateSummary(c.Content, 160)
			if !seen[summary] {
				seen[summary] = true
				blockers = append(blockers, c.Name+": "+summary)
			}
		}
	}
	return blockers
}

// extractDecisionLog scans assistant messages for explicit choice language.
func extractDecisionLog(messages []ConversationMessage) []string {
	var decisions []string
	decisionMarkers := []string{
		"I'll use ", "I chose ", "I'm going with ", "chose ", "decided to ",
		"instead of ", "rather than ", "approach: ",
	}

	for i := len(messages) - 1; i >= 0 && len(decisions) < 3; i-- {
		if messages[i].Role != RoleAssistant {
			continue
		}
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}
		lowered := strings.ToLower(text)
		for _, marker := range decisionMarkers {
			if strings.Contains(lowered, marker) {
				decisions = append(decisions, truncateSummary(text, 160))
				break
			}
		}
	}
	return decisions
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

// HasCompactedPrefix returns true if the message is a compaction summary prefix.
func HasCompactedPrefix(msg ConversationMessage) bool {
	return extractExistingCompactedSummary(msg) != ""
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

// CompactWithLLM is like Compact but uses an LLM summarizer when available.
// If the LLM call fails, it falls back to the heuristic buildIntentSummary.
// When maxHistoryTokens > 0, the summary is capped to fit the history budget.
func CompactWithLLM(ctx context.Context, messages []ConversationMessage, previousSummary string, summarizer *LLMSummarizer, maxHistoryTokens ...int) *CompactionResult {
	if len(messages) <= PreserveLastMessages {
		return nil
	}

	// Determine compaction boundary (same logic as Compact).
	rawKeepFrom := len(messages) - PreserveLastMessages

	compactedPrefixLen := 0
	if len(messages) > 0 && extractExistingCompactedSummary(messages[0]) != "" {
		compactedPrefixLen = 1
	}

	keepFrom := rawKeepFrom
	for keepFrom > compactedPrefixLen {
		firstPreserved := messages[keepFrom]
		startsWithToolResult := len(firstPreserved.Content) > 0 &&
			firstPreserved.Content[0].Type == ContentTypeToolResult

		if !startsWithToolResult {
			break
		}

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
				keepFrom--
				break
			}
		}

		keepFrom--
	}

	// Head protection: find the end of the first HeadProtectedTurns user messages.
	headProtectEnd := findHeadProtectEnd(messages, compactedPrefixLen, keepFrom)

	// Only summarize messages between headProtectEnd and keepFrom.
	toCompact := messages[headProtectEnd:keepFrom]

	// Try LLM summarization first, fall back to heuristic.
	var summary string
	if summarizer != nil {
		var err error
		summary, err = summarizer.Summarize(ctx, toCompact)
		if err != nil {
			slog.Warn("llm summarization failed, falling back to heuristic", "error", err)
			summary = buildIntentSummary(toCompact)
		}
	} else {
		summary = buildIntentSummary(toCompact)
	}

	// Merge with previous summary if present.
	if previousSummary != "" {
		summary = mergeCompactSummaries(previousSummary, summary)
	} else if existingSummary := extractExistingCompactedSummary(messages[0]); existingSummary != "" {
		summary = mergeCompactSummaries(existingSummary, summary)
	}

	// Extract and preserve structured task state across compaction.
	taskState := extractTaskState(toCompact)
	if taskState != "" {
		slog.Info("session.compact.task_state_preserved",
			"state_length", len(taskState),
		)
		summary += "\n\nTask State:\n" + taskState
	}

	// Enforce history budget cap if specified.
	if len(maxHistoryTokens) > 0 && maxHistoryTokens[0] > 0 {
		summary = EnforceSummaryCap(summary, maxHistoryTokens[0])
	}

	formatted := formatCompactSummary(summary)

	return &CompactionResult{
		Summary:            summary,
		FormattedSummary:   formatted,
		PreservedCount:     len(messages) - keepFrom,
		CompactedCount:     len(toCompact),
		PreviousSummary:    previousSummary,
		HeadPreservedCount: headProtectEnd - compactedPrefixLen,
	}
}

// EnforceSummaryCap truncates a summary to fit within maxTokens.
// Uses recursive head/tail splitting: keeps the tail (most recent context)
// and truncates the head. This matches aider's recursive summarization pattern.
func EnforceSummaryCap(summary string, maxTokens int) string {
	if maxTokens <= 0 {
		return summary
	}
	estimated := len(summary)/4 + 1
	if estimated <= maxTokens {
		return summary
	}

	// Truncate to approximately maxTokens * 4 characters.
	maxChars := maxTokens * 4
	if len(summary) <= maxChars {
		return summary
	}

	// Keep the tail (more recent context is more valuable).
	tailChars := maxChars * 2 / 3
	headChars := maxChars - tailChars - 50 // room for the marker

	head := summary[:headChars]
	tail := summary[len(summary)-tailChars:]
	omitted := len(summary) - headChars - tailChars

	return head + fmt.Sprintf("\n[... %d chars of summary omitted to fit history budget ...]\n", omitted) + tail
}

// inferRemainingWork finds user requests that didn't receive a completion response.
func inferRemainingWork(messages []ConversationMessage) []string {
	var remaining []string
	seen := make(map[string]bool)

	for i := 0; i < len(messages) && len(remaining) < 5; i++ {
		if messages[i].Role != RoleUser {
			continue
		}
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}

		// Check if a subsequent assistant message addressed this.
		addressed := false
		for j := i + 1; j < len(messages); j++ {
			if messages[j].Role == RoleAssistant {
				aText := firstTextBlock(messages[j])
				if aText != "" {
					addressed = true
				}
				break
			}
		}
		if !addressed {
			summary := truncateSummary(text, 120)
			if !seen[summary] {
				seen[summary] = true
				remaining = append(remaining, summary)
			}
		}
	}
	return remaining
}

// findHeadProtectEnd returns the index past the first HeadProtectedTurns user
// messages (and their paired assistant responses). If there aren't enough user
// turns in the compaction range, it returns start (no head protection).
func findHeadProtectEnd(messages []ConversationMessage, start, limit int) int {
	userTurnsSeen := 0
	end := start
	for i := start; i < limit; i++ {
		if messages[i].Role == RoleUser {
			// Count only genuine user messages (not tool results acting as user turns).
			hasText := false
			for _, c := range messages[i].Content {
				if c.Type == ContentTypeText {
					hasText = true
					break
				}
			}
			if hasText {
				userTurnsSeen++
			}
		}
		end = i + 1
		if userTurnsSeen >= HeadProtectedTurns {
			// Include the assistant response that follows the last protected user turn.
			for end < limit && messages[end].Role == RoleAssistant {
				end++
			}
			break
		}
	}
	if userTurnsSeen < HeadProtectedTurns {
		// Not enough user turns to protect — don't protect anything.
		return start
	}
	return end
}

// extractResolvedQuestions finds question/answer pairs from user messages
// that received assistant responses.
func extractResolvedQuestions(messages []ConversationMessage) []string {
	var resolved []string
	questionMarkers := []string{"?", "how ", "what ", "why ", "can you ", "could you ", "is there "}

	for i := 0; i < len(messages)-1 && len(resolved) < 5; i++ {
		if messages[i].Role != RoleUser {
			continue
		}
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}
		lowered := strings.ToLower(text)
		isQuestion := false
		for _, marker := range questionMarkers {
			if strings.Contains(lowered, marker) {
				isQuestion = true
				break
			}
		}
		if !isQuestion {
			continue
		}

		// Look for the next assistant response.
		for j := i + 1; j < len(messages); j++ {
			if messages[j].Role == RoleAssistant {
				answer := firstTextBlock(messages[j])
				if answer != "" {
					resolved = append(resolved,
						"Q: "+truncateSummary(text, 100)+" → A: "+truncateSummary(answer, 100))
				}
				break
			}
		}
	}
	return resolved
}

// extractActiveTask finds the most recent user request that hasn't been completed.
// A task is considered incomplete if the last assistant response after it contains
// work-in-progress indicators or if no assistant response follows.
func extractActiveTask(messages []ConversationMessage) string {
	// Walk backward to find the last user request with text content.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != RoleUser {
			continue
		}
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}
		// Check if there's a subsequent assistant message indicating completion.
		completed := false
		for j := i + 1; j < len(messages); j++ {
			if messages[j].Role != RoleAssistant {
				continue
			}
			aText := strings.ToLower(firstTextBlock(messages[j]))
			if strings.Contains(aText, "done") ||
				strings.Contains(aText, "complete") ||
				strings.Contains(aText, "finished") ||
				strings.Contains(aText, "here's the result") ||
				strings.Contains(aText, "successfully") {
				completed = true
			}
			break
		}
		if !completed {
			return truncateSummary(text, 200)
		}
	}
	return ""
}

// extractTaskState identifies structured task state from messages
// (e.g., checkpoint data, tracked variables) and preserves it across compaction.
func extractTaskState(messages []ConversationMessage) string {
	var state []string
	for _, msg := range messages {
		text := firstTextBlock(msg)
		if text == "" {
			continue
		}
		// Look for explicit state markers.
		lower := strings.ToLower(text)
		if strings.Contains(lower, "checkpoint:") ||
			strings.Contains(lower, "state:") ||
			strings.Contains(lower, "progress:") ||
			strings.Contains(lower, "status:") {
			summary := truncateSummary(text, 200)
			if !containsDuplicate(state, summary) {
				state = append(state, summary)
			}
		}
	}
	if len(state) > 5 {
		state = state[len(state)-5:] // keep most recent
	}
	return strings.Join(state, "\n")
}

func containsDuplicate(items []string, item string) bool {
	for _, existing := range items {
		if existing == item {
			return true
		}
	}
	return false
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
