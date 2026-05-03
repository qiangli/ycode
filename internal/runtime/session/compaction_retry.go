package session

import (
	"context"
	"log/slog"
)

const (
	// CompactionMaxRetries is the maximum number of PTL retry attempts.
	// Each retry truncates the oldest API-round group from the messages
	// being compacted. Inspired by Claude Code's truncateHeadForPTLRetry.
	CompactionMaxRetries = 3

	// CompactionTruncationMarker is inserted when messages are truncated for retry.
	CompactionTruncationMarker = "[earlier conversation truncated for compaction retry]"
)

// CompactWithRetry wraps CompactWithLLM with PTL (Prompt Too Long) retry logic.
// If the LLM summarization fails (possibly due to PTL), it retries up to
// CompactionMaxRetries times, each time truncating the oldest API-round group
// from the messages being compacted.
//
// An API-round group is a sequence of: user message + assistant response +
// any tool result messages that follow.
func CompactWithRetry(ctx context.Context, messages []ConversationMessage, previousSummary string, summarizer *LLMSummarizer, maxHistoryTokens ...int) *CompactionResult {
	// First attempt: try with all messages.
	result := CompactWithLLM(ctx, messages, previousSummary, summarizer, maxHistoryTokens...)
	if result != nil {
		return result
	}

	// If CompactWithLLM returned nil, it means too few messages. Fall back.
	if len(messages) <= PreserveLastMessages {
		return nil
	}

	// Retry loop: truncate oldest groups and retry.
	truncated := make([]ConversationMessage, len(messages))
	copy(truncated, messages)

	for attempt := 1; attempt <= CompactionMaxRetries; attempt++ {
		truncated = truncateOldestGroup(truncated)
		if len(truncated) <= PreserveLastMessages {
			break // Can't truncate further.
		}

		slog.Info("compaction retry: truncated oldest group",
			"attempt", attempt,
			"remaining_messages", len(truncated),
		)

		// Insert truncation marker as first message if not already present.
		if len(truncated) > 0 && !hasTruncationMarker(truncated[0]) {
			marker := ConversationMessage{
				Role: RoleSystem,
				Content: []ContentBlock{
					{Type: ContentTypeText, Text: CompactionTruncationMarker},
				},
			}
			truncated = append([]ConversationMessage{marker}, truncated...)
		}

		result = CompactWithLLM(ctx, truncated, previousSummary, summarizer, maxHistoryTokens...)
		if result != nil {
			return result
		}
	}

	// All retries failed — fall back to heuristic compaction.
	slog.Warn("compaction retries exhausted, using heuristic",
		"retries", CompactionMaxRetries,
	)
	return Compact(messages, previousSummary, maxHistoryTokens...)
}

// truncateOldestGroup removes the oldest API-round group from messages.
// A group starts at the first user message (after any system prefix) and
// extends through the next user message (inclusive of assistant + tool results).
func truncateOldestGroup(messages []ConversationMessage) []ConversationMessage {
	if len(messages) <= 1 {
		return messages
	}

	// Skip any leading system messages.
	start := 0
	for start < len(messages) && messages[start].Role == RoleSystem {
		start++
	}

	// Find the end of the first group: from the first user message to the
	// start of the next user text message.
	groupEnd := start + 1
	for groupEnd < len(messages) {
		if messages[groupEnd].Role == RoleUser && hasTextContent(messages[groupEnd]) {
			break
		}
		groupEnd++
	}

	if groupEnd >= len(messages) {
		// Only one group — remove the first non-system message.
		if start < len(messages) {
			return append(messages[:start], messages[start+1:]...)
		}
		return messages
	}

	// Remove messages from start to groupEnd.
	result := make([]ConversationMessage, 0, len(messages)-(groupEnd-start))
	result = append(result, messages[:start]...)
	result = append(result, messages[groupEnd:]...)
	return result
}

// hasTruncationMarker checks if a message is a truncation marker.
func hasTruncationMarker(msg ConversationMessage) bool {
	if msg.Role != RoleSystem {
		return false
	}
	text := firstTextBlock(msg)
	return text == CompactionTruncationMarker
}
