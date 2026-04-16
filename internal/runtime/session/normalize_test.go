package session

import "testing"

func TestNormalizeHistory_ValidHistory(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, ID: "tu1", Name: "read_file"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "tu1", Content: "file content"},
		}},
	}

	result := NormalizeHistory(messages)
	if len(result) != 3 {
		t.Errorf("expected 3 messages (unchanged), got %d", len(result))
	}
}

func TestNormalizeHistory_SynthesizesMissingResult(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, ID: "tu1", Name: "bash"},
		}},
		// Missing tool_result for tu1!
	}

	result := NormalizeHistory(messages)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages (original 2 + synthetic result), got %d", len(result))
	}

	// Last message should be synthetic tool_result.
	last := result[2]
	if last.Role != RoleUser {
		t.Errorf("expected synthetic message role=user, got %s", last.Role)
	}
	if len(last.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(last.Content))
	}
	block := last.Content[0]
	if block.Type != ContentTypeToolResult {
		t.Errorf("expected tool_result, got %s", block.Type)
	}
	if block.ToolUseID != "tu1" {
		t.Errorf("expected tool_use_id=tu1, got %s", block.ToolUseID)
	}
	if !block.IsError {
		t.Error("expected synthetic result to be an error")
	}
}

func TestNormalizeHistory_RemovesOrphanResult(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "orphan1", Content: "stale result"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "response"},
		}},
	}

	result := NormalizeHistory(messages)
	// The first message had only an orphan — should be removed entirely.
	if len(result) != 1 {
		t.Fatalf("expected 1 message (orphan removed), got %d", len(result))
	}
	if result[0].Role != RoleAssistant {
		t.Errorf("expected remaining message to be assistant, got %s", result[0].Role)
	}
}

func TestNormalizeHistory_RemovesOrphanButKeepsOtherBlocks(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "some text"},
			{Type: ContentTypeToolResult, ToolUseID: "orphan1", Content: "orphan"},
		}},
	}

	result := NormalizeHistory(messages)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].Content) != 1 {
		t.Fatalf("expected 1 content block (orphan removed), got %d", len(result[0].Content))
	}
	if result[0].Content[0].Type != ContentTypeText {
		t.Error("expected text block preserved")
	}
}

func TestValidateHistory_ReportsIssues(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, ID: "tu1", Name: "bash"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "orphan1", Content: "orphan"},
		}},
	}

	issues := ValidateHistory(messages)
	if len(issues) != 2 {
		t.Errorf("expected 2 issues (missing result + orphan), got %d: %v", len(issues), issues)
	}
}

func TestMergeAdjacentUserMessages_Basic(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "world"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
	}

	merged := MergeAdjacentUserMessages(messages)
	if len(merged) != 2 {
		t.Fatalf("expected 2 messages after merge, got %d", len(merged))
	}
	if len(merged[0].Content) != 2 {
		t.Errorf("expected 2 content blocks in merged message, got %d", len(merged[0].Content))
	}
}

func TestMergeAdjacentUserMessages_NoMergeToolResult(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "text"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeToolResult, ToolUseID: "t1", Content: "result"}}},
	}

	merged := MergeAdjacentUserMessages(messages)
	if len(merged) != 2 {
		t.Fatalf("should not merge when second message has tool result, got %d messages", len(merged))
	}
}

func TestMergeAdjacentUserMessages_NoMergeAssistant(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "a"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "b"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "c"}}},
	}

	merged := MergeAdjacentUserMessages(messages)
	if len(merged) != 3 {
		t.Fatalf("should not merge across assistant, got %d messages", len(merged))
	}
}

func TestMergeAdjacentUserMessages_Empty(t *testing.T) {
	merged := MergeAdjacentUserMessages(nil)
	if len(merged) != 0 {
		t.Error("nil input should return empty")
	}
}

func TestValidateHistory_Clean(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, ID: "tu1", Name: "bash"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "tu1", Content: "ok"},
		}},
	}

	issues := ValidateHistory(messages)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}
