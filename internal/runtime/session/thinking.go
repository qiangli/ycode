package session

import (
	"strings"
)

const (
	// ThinkingKeepTurns is the number of recent assistant turns whose thinking
	// blocks are preserved. Older thinking blocks are cleared to save context.
	// Inspired by Claude Code's clear_thinking_20251015 strategy.
	ThinkingKeepTurns = 2

	// ThinkingClearedMarker replaces cleared thinking content.
	ThinkingClearedMarker = "[thinking cleared]"

	// ThinkingIdleKeepTurns is used when the session resumes after a long
	// idle period (e.g., >1 hour). Only the most recent thinking turn is kept.
	ThinkingIdleKeepTurns = 1
)

// ClearOldThinking removes thinking/reasoning blocks from older assistant
// messages, keeping only the last `keepTurns` assistant turns with their
// thinking intact. This prevents thinking blocks (which can be 10-30K tokens)
// from accumulating and accelerating context overflow.
//
// Returns a new message slice and the count of cleared thinking blocks.
func ClearOldThinking(messages []ConversationMessage, keepTurns int) ([]ConversationMessage, int) {
	if keepTurns <= 0 {
		keepTurns = ThinkingKeepTurns
	}

	// Find assistant message indices.
	var assistantIndices []int
	for i, msg := range messages {
		if msg.Role == RoleAssistant && hasThinkingBlock(msg) {
			assistantIndices = append(assistantIndices, i)
		}
	}

	if len(assistantIndices) <= keepTurns {
		return messages, 0 // Nothing to clear.
	}

	// Clear thinking in all but the last keepTurns assistant messages.
	clearUpTo := len(assistantIndices) - keepTurns
	toClear := assistantIndices[:clearUpTo]

	result := make([]ConversationMessage, len(messages))
	copy(result, messages)

	cleared := 0
	for _, idx := range toClear {
		msg := result[idx]
		newContent := make([]ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == ContentTypeText && isThinkingContent(block.Text) {
				newContent = append(newContent, ContentBlock{
					Type: ContentTypeText,
					Text: ThinkingClearedMarker,
				})
				cleared++
			} else {
				newContent = append(newContent, block)
			}
		}
		result[idx] = ConversationMessage{
			UUID:      msg.UUID,
			Role:      msg.Role,
			Content:   newContent,
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
			Usage:     msg.Usage,
		}
	}

	return result, cleared
}

// hasThinkingBlock checks if a message contains a thinking/reasoning block.
func hasThinkingBlock(msg ConversationMessage) bool {
	for _, block := range msg.Content {
		if block.Type == ContentTypeText && isThinkingContent(block.Text) {
			return true
		}
	}
	return false
}

// StripMediaBlocks replaces image and document content blocks with
// lightweight markers. This prevents compaction API calls from exceeding
// prompt-too-long limits, especially in multimodal sessions.
//
// Inspired by Claude Code's stripImagesFromMessages().
// Returns a new message slice and the count of stripped blocks.
func StripMediaBlocks(messages []ConversationMessage) ([]ConversationMessage, int) {
	result := make([]ConversationMessage, len(messages))
	stripped := 0

	for i, msg := range messages {
		newContent := make([]ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if isMediaBlock(block) {
				newContent = append(newContent, ContentBlock{
					Type: ContentTypeText,
					Text: mediaMarker(block),
				})
				stripped++
			} else {
				newContent = append(newContent, block)
			}
		}
		result[i] = ConversationMessage{
			UUID:      msg.UUID,
			Role:      msg.Role,
			Content:   newContent,
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
			Usage:     msg.Usage,
		}
	}

	return result, stripped
}

// isMediaBlock checks if a content block is an image or document.
func isMediaBlock(block ContentBlock) bool {
	if block.Type == ContentTypeText {
		text := strings.TrimSpace(block.Text)
		return strings.HasPrefix(text, "data:image/") ||
			strings.HasPrefix(text, "[image:") ||
			strings.HasPrefix(text, "[document:")
	}
	return false
}

// mediaMarker returns a lightweight marker for a media block.
func mediaMarker(block ContentBlock) string {
	text := strings.TrimSpace(block.Text)
	if strings.HasPrefix(text, "data:image/") || strings.HasPrefix(text, "[image:") {
		return "[image]"
	}
	return "[document]"
}
