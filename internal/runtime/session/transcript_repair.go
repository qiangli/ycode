package session

// TranscriptRepair detects and fixes orphaned tool_use/tool_result blocks
// in conversation transcripts. After compaction or pruning, blocks may become
// unpaired — a tool_use without its tool_result, or vice versa. This confuses
// LLMs that expect paired tool interactions.
//
// Inspired by openclaw's session-transcript-repair.ts which auto-detects
// orphaned tool-use blocks and redacts sensitive attachments.

const prunedToolResult = "[result pruned during compaction]"

// RepairStats records what was fixed during transcript repair.
type RepairStats struct {
	OrphanedToolUse    int // tool_use blocks with no matching tool_result
	OrphanedToolResult int // tool_result blocks with no matching tool_use
}

// RepairTranscript scans messages for unpaired tool_use/tool_result blocks
// and fixes them in place. For orphaned tool_use blocks, a synthetic
// tool_result is appended to the next user message. For orphaned
// tool_result blocks, they are removed.
//
// Messages are modified in place. Returns stats about what was repaired.
func RepairTranscript(messages []ConversationMessage) RepairStats {
	var stats RepairStats

	// Collect all tool_use IDs and tool_result references.
	toolUseIDs := make(map[string]bool)
	toolResultRefs := make(map[string]bool)

	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeToolUse:
				if block.ID != "" {
					toolUseIDs[block.ID] = true
				}
			case ContentTypeToolResult:
				if block.ToolUseID != "" {
					toolResultRefs[block.ToolUseID] = true
				}
			}
		}
	}

	// Find orphaned tool_use blocks (no matching tool_result).
	orphanedUses := make(map[string]bool)
	for id := range toolUseIDs {
		if !toolResultRefs[id] {
			orphanedUses[id] = true
		}
	}

	// Find orphaned tool_result blocks (no matching tool_use).
	orphanedResults := make(map[string]bool)
	for ref := range toolResultRefs {
		if !toolUseIDs[ref] {
			orphanedResults[ref] = true
		}
	}

	if len(orphanedUses) == 0 && len(orphanedResults) == 0 {
		return stats
	}

	// Fix orphaned tool_use: inject synthetic tool_result into the next user message.
	if len(orphanedUses) > 0 {
		for i, msg := range messages {
			if msg.Role != RoleAssistant {
				continue
			}
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolUse && orphanedUses[block.ID] {
					// Find the next user message to inject the synthetic result.
					injected := false
					for j := i + 1; j < len(messages); j++ {
						if messages[j].Role == RoleUser {
							messages[j].Content = append([]ContentBlock{{
								Type:      ContentTypeToolResult,
								ToolUseID: block.ID,
								Content:   prunedToolResult,
							}}, messages[j].Content...)
							injected = true
							break
						}
					}
					if !injected {
						// No subsequent user message; append a new one.
						messages = append(messages, ConversationMessage{
							Role: RoleUser,
							Content: []ContentBlock{{
								Type:      ContentTypeToolResult,
								ToolUseID: block.ID,
								Content:   prunedToolResult,
							}},
						})
					}
					stats.OrphanedToolUse++
				}
			}
		}
	}

	// Fix orphaned tool_result: remove from messages.
	if len(orphanedResults) > 0 {
		for i := range messages {
			var kept []ContentBlock
			for _, block := range messages[i].Content {
				if block.Type == ContentTypeToolResult && orphanedResults[block.ToolUseID] {
					stats.OrphanedToolResult++
					continue
				}
				kept = append(kept, block)
			}
			messages[i].Content = kept
		}
	}

	return stats
}
