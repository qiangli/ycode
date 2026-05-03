package session

// MicrocompactionKeepRecent is the default number of recent tool use/result pairs to keep.
const MicrocompactionKeepRecent = 20

// MicrocompactionThreshold is the estimated token count above which
// microcompaction triggers (between pruning and full compaction).
const MicrocompactionThreshold = 150_000

// ClearOldToolUses clears old tool_use inputs and tool_result outputs from
// conversation messages while preserving the message structure for API validity.
// It keeps the most recent `keepRecent` tool use/result pairs intact.
//
// This is "Layer 1.5" — cheaper than LLM-based compaction because it runs
// deterministically without any LLM call. Inspired by Claude Code's
// clear_tool_uses_20250919 native API strategy.
//
// Returns the modified messages and the count of cleared pairs.
func ClearOldToolUses(messages []ConversationMessage, keepRecent int) ([]ConversationMessage, int) {
	if keepRecent <= 0 {
		keepRecent = MicrocompactionKeepRecent
	}

	// Find all tool_use/tool_result message indices.
	type toolPair struct {
		useIdx    int
		resultIdx int
	}

	var pairs []toolPair

	// Scan for tool_use messages and their corresponding tool_result messages.
	for i, msg := range messages {
		if msg.Role == "assistant" && mcHasToolUse(msg) {
			// Find the corresponding tool_result (typically the next user message).
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "user" && mcHasToolResult(messages[j]) {
					pairs = append(pairs, toolPair{useIdx: i, resultIdx: j})
					break
				}
			}
		}
	}

	if len(pairs) <= keepRecent {
		return messages, 0 // nothing to clear
	}

	// Clear older pairs (keep the most recent keepRecent).
	clearCount := len(pairs) - keepRecent
	toClear := pairs[:clearCount]

	// Make a copy to avoid mutating the original.
	result := make([]ConversationMessage, len(messages))
	copy(result, messages)

	cleared := 0
	for _, pair := range toClear {
		// Clear tool_use input.
		if pair.useIdx < len(result) {
			result[pair.useIdx] = clearToolUseContent(result[pair.useIdx])
		}
		// Clear tool_result output.
		if pair.resultIdx < len(result) {
			result[pair.resultIdx] = clearToolResultContent(result[pair.resultIdx])
		}
		cleared++
	}

	return result, cleared
}

// mcHasToolUse checks if a message contains a tool_use content block.
func mcHasToolUse(msg ConversationMessage) bool {
	for _, block := range msg.Content {
		if block.Type == ContentTypeToolUse {
			return true
		}
	}
	return false
}

// mcHasToolResult checks if a message contains a tool_result content block.
func mcHasToolResult(msg ConversationMessage) bool {
	for _, block := range msg.Content {
		if block.Type == ContentTypeToolResult {
			return true
		}
	}
	return false
}

// clearToolUseContent replaces tool_use inputs with empty objects.
func clearToolUseContent(msg ConversationMessage) ConversationMessage {
	cleared := msg
	cleared.Content = make([]ContentBlock, len(msg.Content))
	copy(cleared.Content, msg.Content)

	for i, block := range cleared.Content {
		if block.Type == ContentTypeToolUse {
			cleared.Content[i].Input = []byte("{}")
		}
	}
	return cleared
}

// clearToolResultContent replaces tool_result content with a placeholder.
func clearToolResultContent(msg ConversationMessage) ConversationMessage {
	cleared := msg
	cleared.Content = make([]ContentBlock, len(msg.Content))
	copy(cleared.Content, msg.Content)

	for i, block := range cleared.Content {
		if block.Type == ContentTypeToolResult {
			cleared.Content[i].Content = "[cleared]"
		}
	}
	return cleared
}
