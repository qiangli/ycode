package session

import (
	"testing"
)

func TestClearOldToolUses_NoChange(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Content: "hello"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeToolUse, Name: "bash", ID: "t1"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeToolResult, ToolUseID: "t1", Content: "output"}}},
	}

	result, cleared := ClearOldToolUses(messages, 20)
	if cleared != 0 {
		t.Fatalf("expected 0 cleared, got %d", cleared)
	}
	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}
}

func TestClearOldToolUses_ClearsOld(t *testing.T) {
	var messages []ConversationMessage

	// Create 25 tool use/result pairs.
	for i := 0; i < 25; i++ {
		messages = append(messages,
			ConversationMessage{
				Role:    RoleAssistant,
				Content: []ContentBlock{{Type: ContentTypeToolUse, Name: "bash", ID: "t" + string(rune('a'+i))}},
			},
			ConversationMessage{
				Role:    RoleUser,
				Content: []ContentBlock{{Type: ContentTypeToolResult, ToolUseID: "t" + string(rune('a'+i)), Content: "output " + string(rune('a'+i))}},
			},
		)
	}

	result, cleared := ClearOldToolUses(messages, 20)
	if cleared != 5 {
		t.Fatalf("expected 5 cleared (25-20), got %d", cleared)
	}

	// The first 5 tool results should be "[cleared]".
	clearedCount := 0
	for _, msg := range result {
		for _, block := range msg.Content {
			if block.Type == ContentTypeToolResult && block.Content == "[cleared]" {
				clearedCount++
			}
		}
	}
	if clearedCount != 5 {
		t.Fatalf("expected 5 cleared results, got %d", clearedCount)
	}
}

func TestClearOldToolUses_PreservesRecent(t *testing.T) {
	var messages []ConversationMessage

	for i := 0; i < 5; i++ {
		messages = append(messages,
			ConversationMessage{
				Role:    RoleAssistant,
				Content: []ContentBlock{{Type: ContentTypeToolUse, Name: "read_file", ID: "r" + string(rune('0'+i))}},
			},
			ConversationMessage{
				Role:    RoleUser,
				Content: []ContentBlock{{Type: ContentTypeToolResult, ToolUseID: "r" + string(rune('0'+i)), Content: "file content"}},
			},
		)
	}

	result, cleared := ClearOldToolUses(messages, 5)
	if cleared != 0 {
		t.Fatalf("expected 0 cleared (exactly at limit), got %d", cleared)
	}
	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}
}

func TestClearOldToolUses_PreservesTextBlocks(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Content: "important context"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Content: "I'll check that"},
			{Type: ContentTypeToolUse, Name: "bash", ID: "t1"},
		}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeToolResult, ToolUseID: "t1", Content: "result"}}},
	}

	result, _ := ClearOldToolUses(messages, 0)

	// Text blocks should be preserved even if tool content is cleared.
	if result[0].Content[0].Content != "important context" {
		t.Error("user text block should be preserved")
	}
}
