package session

import (
	"testing"
)

func TestNeedsCompaction(t *testing.T) {
	if NeedsCompaction(50_000) {
		t.Error("50K tokens should not need compaction")
	}
	if !NeedsCompaction(150_000) {
		t.Error("150K tokens should need compaction")
	}
}

func TestCompact_TooFewMessages(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
	}

	result := Compact(messages, "")
	if result != nil {
		t.Error("should return nil when message count <= PreserveLastMessages")
	}
}

func TestCompact_ProducesSummary(t *testing.T) {
	messages := make([]ConversationMessage, 10)
	for i := 0; i < 10; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		messages[i] = ConversationMessage{
			Role: role,
			Content: []ContentBlock{
				{Type: ContentTypeText, Text: "message " + string(rune('0'+i))},
			},
		}
	}

	result := Compact(messages, "")
	if result == nil {
		t.Fatal("compact should produce a result")
	}
	if result.Summary == "" {
		t.Error("summary should not be empty")
	}
	if result.PreservedCount != PreserveLastMessages {
		t.Errorf("expected %d preserved, got %d", PreserveLastMessages, result.PreservedCount)
	}
	if result.CompactedCount != len(messages)-PreserveLastMessages {
		t.Errorf("expected %d compacted, got %d", len(messages)-PreserveLastMessages, result.CompactedCount)
	}
}

func TestCompact_WithPreviousSummary(t *testing.T) {
	messages := make([]ConversationMessage, 8)
	for i := range messages {
		messages[i] = ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "test"}},
		}
	}

	result := Compact(messages, "Previous context: we were debugging a bug")
	if result == nil {
		t.Fatal("compact should produce a result")
	}
	if result.PreviousSummary != "Previous context: we were debugging a bug" {
		t.Error("previous summary should be preserved")
	}
}

func TestSummarizeMessages_IncludesScope(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "fix the bug"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "I'll fix it"},
			{Type: ContentTypeToolUse, Name: "read_file"},
		}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "thanks"}}},
	}

	summary := summarizeMessages(messages)
	if summary == "" {
		t.Fatal("summary should not be empty")
	}
	// Should contain scope section.
	if !contains(summary, "Scope") {
		t.Error("summary should contain Scope section")
	}
	// Should contain summary XML tags.
	if !contains(summary, "<summary>") {
		t.Error("summary should contain <summary> tag")
	}
	// Should contain key timeline.
	if !contains(summary, "Key timeline:") {
		t.Error("summary should contain Key timeline section")
	}
	// Should contain tools mentioned.
	if !contains(summary, "read_file") {
		t.Error("summary should mention tools used")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
