package session

// NormalizeHistory ensures tool-use/tool-result pairing invariants.
// For every tool_use block with an ID, there must be a corresponding
// tool_result block with a matching tool_use_id. Missing results are
// synthesized as aborted. Orphan results (no matching tool_use) are removed.
func NormalizeHistory(messages []ConversationMessage) []ConversationMessage {
	// Phase 1: Collect all tool_use IDs and tool_result tool_use_ids.
	toolUseIDs := make(map[string]bool)    // ID → exists
	toolResultIDs := make(map[string]bool) // tool_use_id → exists

	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeToolUse:
				if block.ID != "" {
					toolUseIDs[block.ID] = true
				}
			case ContentTypeToolResult:
				if block.ToolUseID != "" {
					toolResultIDs[block.ToolUseID] = true
				}
			}
		}
	}

	// Phase 2: Find missing results (tool_use with no matching tool_result).
	missingResults := make(map[string]bool)
	for id := range toolUseIDs {
		if !toolResultIDs[id] {
			missingResults[id] = true
		}
	}

	// Phase 3: Find orphan results (tool_result with no matching tool_use).
	orphanResults := make(map[string]bool)
	for id := range toolResultIDs {
		if !toolUseIDs[id] {
			orphanResults[id] = true
		}
	}

	// Phase 4: Build normalized message list.
	result := make([]ConversationMessage, 0, len(messages))

	for _, msg := range messages {
		// Filter out orphan tool_result blocks.
		hasOrphans := false
		for _, block := range msg.Content {
			if block.Type == ContentTypeToolResult && orphanResults[block.ToolUseID] {
				hasOrphans = true
				break
			}
		}

		if hasOrphans {
			var filteredBlocks []ContentBlock
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolResult && orphanResults[block.ToolUseID] {
					continue
				}
				filteredBlocks = append(filteredBlocks, block)
			}
			if len(filteredBlocks) == 0 {
				continue // Skip entirely empty messages.
			}
			msg = ConversationMessage{
				UUID:      msg.UUID,
				Role:      msg.Role,
				Content:   filteredBlocks,
				Timestamp: msg.Timestamp,
				Model:     msg.Model,
				Usage:     msg.Usage,
			}
		}

		result = append(result, msg)
	}

	// Phase 5: Synthesize missing tool_result blocks.
	// Find the assistant message containing each missing tool_use and insert
	// a user message with the synthetic result after it.
	if len(missingResults) > 0 {
		var withSynthetic []ConversationMessage
		for _, msg := range result {
			withSynthetic = append(withSynthetic, msg)

			if msg.Role != RoleAssistant {
				continue
			}

			var syntheticBlocks []ContentBlock
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolUse && missingResults[block.ID] {
					syntheticBlocks = append(syntheticBlocks, ContentBlock{
						Type:      ContentTypeToolResult,
						ToolUseID: block.ID,
						Content:   "[Aborted: tool execution was interrupted]",
						IsError:   true,
					})
				}
			}

			if len(syntheticBlocks) > 0 {
				withSynthetic = append(withSynthetic, ConversationMessage{
					Role:      RoleUser,
					Content:   syntheticBlocks,
					Timestamp: msg.Timestamp,
				})
			}
		}
		result = withSynthetic
	}

	return result
}

// MergeAdjacentUserMessages merges consecutive user-role messages into single
// messages by concatenating their content blocks. This eliminates per-message
// structural overhead (role tokens, boundary framing) when system reminders or
// dynamic injections are stored as separate user messages.
//
// Messages containing tool results are never merged (they're linked to specific
// tool_use IDs). Only pure text user messages are candidates for merging.
//
// Inspired by Kimi CLI's normalize_history() in dynamic_injection.py.
func MergeAdjacentUserMessages(messages []ConversationMessage) []ConversationMessage {
	if len(messages) <= 1 {
		return messages
	}

	result := make([]ConversationMessage, 0, len(messages))

	for _, msg := range messages {
		// Only merge user messages that don't contain tool results.
		canMerge := msg.Role == RoleUser && !hasToolResult(msg)

		if canMerge && len(result) > 0 {
			prev := &result[len(result)-1]
			if prev.Role == RoleUser && !hasToolResult(*prev) {
				// Merge: append this message's content to the previous one.
				prev.Content = append(prev.Content, msg.Content...)
				continue
			}
		}

		result = append(result, msg)
	}

	return result
}

// hasToolResult checks if a message contains any tool result content blocks.
func hasToolResult(msg ConversationMessage) bool {
	for _, b := range msg.Content {
		if b.Type == ContentTypeToolResult {
			return true
		}
	}
	return false
}

// ValidateHistory returns a list of issues found in the message history.
func ValidateHistory(messages []ConversationMessage) []string {
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)

	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeToolUse:
				if block.ID != "" {
					toolUseIDs[block.ID] = true
				}
			case ContentTypeToolResult:
				if block.ToolUseID != "" {
					toolResultIDs[block.ToolUseID] = true
				}
			}
		}
	}

	var issues []string
	for id := range toolUseIDs {
		if !toolResultIDs[id] {
			issues = append(issues, "missing tool_result for tool_use ID: "+id)
		}
	}
	for id := range toolResultIDs {
		if !toolUseIDs[id] {
			issues = append(issues, "orphan tool_result with tool_use_id: "+id)
		}
	}
	return issues
}
