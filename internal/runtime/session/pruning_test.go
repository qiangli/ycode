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

	pruned, result := PruneMessages(messages, big200K())
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

	pruned, result := PruneMessages(messages, big200K())
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

	pruned, result := PruneMessages(messages, big200K())
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

	PruneMessages(messages, big200K())

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

// big200K pins a compaction threshold of exactly 100K — the number the ratio
// fixtures in this file were written against (60% warn / 80% critical / 100%
// overflow). These tests are about the RATIO logic, so the threshold is stated
// outright rather than derived, and stays stable if the derivation changes.
func big200K() ContextBudget {
	return ContextBudget{
		ContextWindow:       200_000,
		ReservedTokens:      40_000,
		CompactionThreshold: 100_000,
	}
}

func TestEstimateTextTokens_ASCII(t *testing.T) {
	// 100 ASCII chars ≈ 25 tokens (0.25 per char).
	text := strings.Repeat("a", 100)
	tokens := EstimateTextTokens(text)
	if tokens < 25 || tokens > 30 {
		t.Errorf("expected ~25 tokens for 100 ASCII chars, got %d", tokens)
	}
}

func TestEstimateTextTokens_CJK(t *testing.T) {
	// 100 CJK chars ≈ 130 tokens (1.3 per char).
	text := strings.Repeat("你", 100)
	tokens := EstimateTextTokens(text)
	if tokens < 125 || tokens > 140 {
		t.Errorf("expected ~130 tokens for 100 CJK chars, got %d", tokens)
	}
}

func TestEstimateTextTokens_Mixed(t *testing.T) {
	text := "Hello 世界" // 6 ASCII + 1 space + 2 CJK
	tokens := EstimateTextTokens(text)
	// 7 ASCII × 0.25 + 2 CJK × 1.3 = 1.75 + 2.6 = 4.35 → 5
	if tokens < 4 || tokens > 7 {
		t.Errorf("expected ~5 tokens for mixed text, got %d", tokens)
	}
}

func TestEstimateTextTokens_LargeFallback(t *testing.T) {
	// Above 100K chars should use fast len/4 fallback.
	text := strings.Repeat("x", 200_000)
	tokens := EstimateTextTokens(text)
	expected := 200_000/4 + 1
	if tokens != expected {
		t.Errorf("expected %d tokens for large text, got %d", expected, tokens)
	}
}

func TestSoftTrimRatios(t *testing.T) {
	// Verify the ratios produce expected values.
	if SoftTrimHeadChars >= SoftTrimTailChars {
		t.Errorf("head (%d) should be less than tail (%d)", SoftTrimHeadChars, SoftTrimTailChars)
	}
	if SoftTrimHeadChars+SoftTrimTailChars != SoftTrimTotalChars {
		t.Errorf("head (%d) + tail (%d) should equal total (%d)",
			SoftTrimHeadChars, SoftTrimTailChars, SoftTrimTotalChars)
	}
	// Head should be ~15% of total.
	headPct := float64(SoftTrimHeadChars) / float64(SoftTrimTotalChars)
	if headPct < 0.10 || headPct > 0.20 {
		t.Errorf("head ratio %.2f should be ~0.15", headPct)
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
