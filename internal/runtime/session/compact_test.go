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

func TestBuildIntentSummary_IncludesStructuredFields(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "fix the bug in auth.go"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "I'll use the edit approach instead of rewriting"},
			{Type: ContentTypeToolUse, Name: "read_file"},
		}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeToolResult, Name: "read_file", Content: "file contents"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "edit_file", Input: []byte(`{"path":"internal/auth.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Name: "edit_file", Content: "edited internal/auth.go"},
		}},
	}

	summary := buildIntentSummary(messages)
	if summary == "" {
		t.Fatal("summary should not be empty")
	}
	// Should contain intent_summary tags.
	if !contains(summary, "<intent_summary>") {
		t.Error("summary should contain <intent_summary> tag")
	}
	// Should contain Scope.
	if !contains(summary, "Scope") {
		t.Error("summary should contain Scope")
	}
	// Should contain Primary Goal.
	if !contains(summary, "Primary Goal") {
		t.Error("summary should contain Primary Goal")
	}
	// Should contain tools.
	if !contains(summary, "read_file") || !contains(summary, "edit_file") {
		t.Error("summary should mention tools used")
	}
	// Should contain Decision Log (assistant used "instead of").
	if !contains(summary, "Decision Log") {
		t.Error("summary should contain Decision Log")
	}
}

func TestBuildIntentSummary_ActiveBlockers(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "run the tests"}}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Name: "bash", Content: "FAIL: TestAuth", IsError: true},
		}},
	}

	summary := buildIntentSummary(messages)
	if !contains(summary, "Active Blockers") {
		t.Error("summary should contain Active Blockers when errors present")
	}
	if !contains(summary, "FAIL") {
		t.Error("summary should include error content in blockers")
	}
}

func TestBuildIntentSummary_WorkingSet(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "write_file", Input: []byte(`{"path": "internal/api/client.go", "content": "package api"}`)},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "edit_file", Input: []byte(`{"path": "internal/api/server.go", "old_string": "a", "new_string": "b"}`)},
		}},
	}

	summary := buildIntentSummary(messages)
	if !contains(summary, "Working Set") {
		t.Errorf("summary should contain Working Set for write/edit operations, got:\n%s", summary)
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
