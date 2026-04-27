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
	expectedCompacted := len(messages) - PreserveLastMessages - result.HeadPreservedCount
	if result.CompactedCount != expectedCompacted {
		t.Errorf("expected %d compacted, got %d (head preserved: %d)",
			expectedCompacted, result.CompactedCount, result.HeadPreservedCount)
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

func TestCompact_HeadProtection(t *testing.T) {
	// Create messages: 2 user turns at the head + several more messages.
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "I want to refactor the auth module"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Sure, I'll help with that"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Use the strategy pattern please"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Good idea, let me start"}}},
		// These should be compacted (middle section).
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Also rename the config struct"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Done with the rename"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Now add error handling"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Added error handling"}}},
		// Tail preserved messages (last PreserveLastMessages).
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Run the tests"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Tests pass"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Great, commit it"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Committed"}}},
	}

	result := Compact(messages, "")
	if result == nil {
		t.Fatal("compact should produce a result")
	}

	// Head protection should preserve the first 2 user turns + their responses.
	if result.HeadPreservedCount == 0 {
		t.Error("HeadPreservedCount should be > 0 when there are enough user turns")
	}

	// The head-protected messages (first 2 user turns + responses = 4 messages)
	// should NOT be in the compacted count.
	// Total messages: 12
	// Tail preserved: PreserveLastMessages (4)
	// Head preserved: 4 (2 user turns + 2 assistant responses)
	// Compacted: 12 - 4 (tail) - 4 (head) = 4
	expectedHeadPreserved := 4
	if result.HeadPreservedCount != expectedHeadPreserved {
		t.Errorf("HeadPreservedCount = %d, want %d", result.HeadPreservedCount, expectedHeadPreserved)
	}

	expectedCompacted := len(messages) - PreserveLastMessages - expectedHeadPreserved
	if result.CompactedCount != expectedCompacted {
		t.Errorf("CompactedCount = %d, want %d", result.CompactedCount, expectedCompacted)
	}

	if result.PreservedCount != PreserveLastMessages {
		t.Errorf("PreservedCount = %d, want %d", result.PreservedCount, PreserveLastMessages)
	}
}

func TestCompact_HeadProtection_TooFewUserTurns(t *testing.T) {
	// Only 1 user turn — head protection should not activate.
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "more"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "stuff"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "bye"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "goodbye"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "wait"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "yes?"}}},
	}

	result := Compact(messages, "")
	if result == nil {
		t.Fatal("compact should produce a result")
	}

	// Only 1 user turn before the tail-preserved section is reachable for head
	// protection, so it should fall below HeadProtectedTurns (2) and not activate.
	// The exact value depends on where keepFrom lands, but HeadPreservedCount
	// should be 0 if there aren't enough user turns.
	// Actually there are 3 user turns total, and keepFrom = 8-4=4, so within
	// indices 0..3 we have user at 0 (1 turn) — not enough.
	if result.HeadPreservedCount != 0 {
		t.Errorf("HeadPreservedCount = %d, want 0 when too few user turns", result.HeadPreservedCount)
	}
}

func TestExtractResolvedQuestions(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "How do I fix the auth bug?"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "You need to update the token validator"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Please refactor the module"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Done"}}},
	}

	resolved := extractResolvedQuestions(messages)
	if len(resolved) == 0 {
		t.Error("should extract at least one resolved question")
	}
	// The question "How do I fix..." should be resolved.
	found := false
	for _, q := range resolved {
		if contains(q, "Q:") && contains(q, "A:") {
			found = true
			break
		}
	}
	if !found {
		t.Error("resolved questions should have Q/A format")
	}
}

func TestExtractActiveTask(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Fix the bug"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Working on it..."}}},
	}

	task := extractActiveTask(messages)
	if task == "" {
		t.Error("should find an active task when assistant hasn't completed")
	}
	if !contains(task, "Fix the bug") {
		t.Errorf("active task should be the user request, got: %s", task)
	}
}

func TestExtractActiveTask_Completed(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "Fix the bug"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "Done, I've successfully fixed the issue"}}},
	}

	task := extractActiveTask(messages)
	if task != "" {
		t.Errorf("should not find an active task when completed, got: %s", task)
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
