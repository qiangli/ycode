package session

import (
	"context"
	"testing"
)

func TestTruncateOldestGroup_SingleGroup(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "do something"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "done"}}},
	}

	result := truncateOldestGroup(messages)
	// Should remove the first user message.
	if len(result) >= len(messages) {
		t.Errorf("expected fewer messages after truncation, got %d", len(result))
	}
}

func TestTruncateOldestGroup_MultipleGroups(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "first request"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "response 1"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "second request"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "response 2"}}},
	}

	result := truncateOldestGroup(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 remaining messages, got %d", len(result))
	}
	// The second group should remain.
	if result[0].Content[0].Text != "second request" {
		t.Errorf("expected 'second request', got %s", result[0].Content[0].Text)
	}
}

func TestTruncateOldestGroup_WithSystemPrefix(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentTypeText, Text: "system prompt"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "first"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "response"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "second"}}},
	}

	result := truncateOldestGroup(messages)
	// System message should be preserved.
	if result[0].Role != RoleSystem {
		t.Error("system message should be preserved")
	}
	// First group should be removed.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (system + second user), got %d", len(result))
	}
}

func TestTruncateOldestGroup_WithToolResults(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "search"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "bash", ID: "t1"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "t1", Content: "result"},
		}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "next task"}}},
	}

	result := truncateOldestGroup(messages)
	// Should remove first user + assistant + tool result, keep "next task".
	if len(result) != 1 {
		t.Fatalf("expected 1 remaining message, got %d", len(result))
	}
	if result[0].Content[0].Text != "next task" {
		t.Errorf("expected 'next task', got %s", result[0].Content[0].Text)
	}
}

func TestTruncateOldestGroup_Empty(t *testing.T) {
	result := truncateOldestGroup(nil)
	if len(result) != 0 {
		t.Error("empty input should return empty")
	}
}

func TestHasTruncationMarker(t *testing.T) {
	marker := ConversationMessage{
		Role: RoleSystem,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: CompactionTruncationMarker},
		},
	}
	if !hasTruncationMarker(marker) {
		t.Error("should detect truncation marker")
	}

	notMarker := ConversationMessage{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "regular message"},
		},
	}
	if hasTruncationMarker(notMarker) {
		t.Error("should not detect marker in regular message")
	}
}

func TestCompactWithRetry_FallsBackToHeuristic(t *testing.T) {
	// With nil summarizer, CompactWithRetry should fall back to heuristic Compact.
	messages := make([]ConversationMessage, 10)
	for i := range messages {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		messages[i] = ConversationMessage{
			Role:    role,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "message"}},
		}
	}

	result := CompactWithRetry(context.TODO(), messages, "", nil)
	if result == nil {
		t.Fatal("should produce a result via heuristic fallback")
	}
	if result.Summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestCompactWithRetry_TooFewMessages(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
	}

	result := CompactWithRetry(context.TODO(), messages, "", nil)
	if result != nil {
		t.Error("should return nil for too few messages")
	}
}
