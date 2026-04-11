package session

import (
	"strings"
	"testing"
)

func TestPruneMessages_NoOpBelowThreshold(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
	}

	pruned, result := PruneMessages(messages)
	if result != nil {
		t.Error("should return nil result when below threshold")
	}
	if len(pruned) != len(messages) {
		t.Error("should return same messages")
	}
}

func TestPruneMessages_SoftTrimsLargeToolResults(t *testing.T) {
	// Create messages that exceed the soft trim threshold (~60K tokens = ~240K chars).
	largeContent := strings.Repeat("x", 300_000) // ~75K tokens

	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "search files"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "grep_search", ID: "t1"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "t1", Content: largeContent},
		}},
		// Recent messages (protected).
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "now fix it"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "ok"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "thanks"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "done"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "great"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "bye"}}},
	}

	pruned, result := PruneMessages(messages)
	if result == nil {
		t.Fatal("should have pruned")
	}
	if result.SoftTrimmed+result.HardCleared == 0 {
		t.Error("should have trimmed at least one tool result")
	}
	if result.TokensAfter >= result.TokensBefore {
		t.Error("tokens should decrease after pruning")
	}

	// Verify the tool result was modified.
	prunedContent := pruned[2].Content[0].Content
	if prunedContent == largeContent {
		t.Error("tool result should have been modified")
	}
}

func TestPruneMessages_ProtectsRecentMessages(t *testing.T) {
	largeContent := strings.Repeat("x", 300_000)

	// The large tool result is in the last RecentMessagesProtected messages — should NOT be pruned.
	messages := make([]ConversationMessage, RecentMessagesProtected)
	messages[0] = ConversationMessage{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "t1", Content: largeContent},
		},
	}
	for i := 1; i < RecentMessagesProtected; i++ {
		messages[i] = ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "msg"}},
		}
	}

	pruned, result := PruneMessages(messages)
	if result != nil && (result.SoftTrimmed > 0 || result.HardCleared > 0) {
		t.Error("should not prune protected recent messages")
	}
	// Content should be unchanged.
	if pruned[0].Content[0].Content != largeContent {
		t.Error("protected content should be unchanged")
	}
}

func TestPruneMessages_DoesNotModifyOriginal(t *testing.T) {
	largeContent := strings.Repeat("y", 300_000)

	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "t1", Content: largeContent},
		}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "a"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "b"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "c"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "d"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "e"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "f"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "g"}}},
	}

	PruneMessages(messages)

	// Original should be untouched.
	if messages[0].Content[0].Content != largeContent {
		t.Error("original messages should not be modified")
	}
}

func TestSoftTrim(t *testing.T) {
	content := strings.Repeat("a", 1000)
	trimmed := softTrim(content)

	if len(trimmed) >= len(content) {
		t.Error("trimmed should be shorter than original")
	}
	if !strings.Contains(trimmed, "characters omitted") {
		t.Error("should contain omission marker")
	}
	// Should start with head content.
	if !strings.HasPrefix(trimmed, strings.Repeat("a", SoftTrimHeadChars)) {
		t.Error("should preserve head")
	}
	// Should end with tail content.
	if !strings.HasSuffix(trimmed, strings.Repeat("a", SoftTrimTailChars)) {
		t.Error("should preserve tail")
	}
}

func TestCheckContextHealth(t *testing.T) {
	tests := []struct {
		name     string
		tokens   int // approximate chars = tokens * 4
		expected ContextLevel
	}{
		{"healthy", 10_000, ContextHealthy},    // ~40K chars
		{"warning", 65_000, ContextWarning},    // ~260K chars, > 60% of 100K
		{"critical", 85_000, ContextCritical},  // ~340K chars, > 80% of 100K
		{"overflow", 110_000, ContextOverflow}, // ~440K chars, > 100% of 100K
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a message with enough text to hit the target token count.
			text := strings.Repeat("x", tt.tokens*4)
			messages := []ConversationMessage{
				{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: text}}},
			}

			health := CheckContextHealth(messages)
			if health.Level != tt.expected {
				t.Errorf("expected %s, got %s (tokens=%d, ratio=%.2f)",
					tt.expected, health.Level, health.EstimatedTokens, health.Ratio)
			}
		})
	}
}

func TestContextHealth_String(t *testing.T) {
	h := ContextHealth{
		EstimatedTokens: 75000,
		Threshold:       100000,
		Ratio:           0.75,
		Level:           ContextWarning,
	}

	s := h.String()
	if !strings.Contains(s, "75k") {
		t.Errorf("should contain token count: %s", s)
	}
	if !strings.Contains(s, "warning") {
		t.Errorf("should contain level: %s", s)
	}
}
