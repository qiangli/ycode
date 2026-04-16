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

func TestMaskOldObservations_WindowParameter(t *testing.T) {
	// Create 12 messages with tool results. Content must be longer than
	// maskedPlaceholder ("<MASKED>") to be eligible for masking.
	var messages []ConversationMessage
	for i := range 12 {
		messages = append(messages, ConversationMessage{
			Role: RoleUser,
			Content: []ContentBlock{
				{Type: ContentTypeToolResult, ToolUseID: "t" + strings.Repeat("x", i),
					Content: "This is a sufficiently long tool result content for masking test #" + strings.Repeat("x", 20+i)},
			},
		})
	}

	// Window=10: should mask 2 (12-10).
	_, maskedNormal := MaskOldObservations(messages, ObservationMaskingWindow)
	// Window=6: should mask 6 (12-6).
	_, maskedAggressive := MaskOldObservations(messages, ObservationMaskingWindowAggressive)

	if maskedNormal != 2 {
		t.Errorf("window=10: expected 2 masked, got %d", maskedNormal)
	}
	if maskedAggressive != 6 {
		t.Errorf("window=6: expected 6 masked, got %d", maskedAggressive)
	}
}

func TestMaskOldObservations_NothingToMask(t *testing.T) {
	var messages []ConversationMessage
	for range 5 {
		messages = append(messages, ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeToolResult, ToolUseID: "t", Content: "result"}},
		})
	}

	_, masked := MaskOldObservations(messages, 10)
	if masked != 0 {
		t.Errorf("expected 0 masked when within window, got %d", masked)
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

func TestMaskOldObservationsBudget_Basic(t *testing.T) {
	// Create messages with large tool results that exceed protection + prunable thresholds.
	// 40 messages × 20K chars each ≈ 5K tokens each = 200K total tokens.
	// Protection: 50K, Prunable threshold: 30K → should mask oldest results.
	var messages []ConversationMessage
	for i := range 40 {
		content := strings.Repeat("x", 20000) // ~5000 tokens each
		messages = append(messages, ConversationMessage{
			Role: RoleUser,
			Content: []ContentBlock{
				{Type: ContentTypeToolResult, ToolUseID: "t" + strings.Repeat("0", i+1),
					Name: "bash", Content: content},
			},
		})
	}

	// With protection=50K, prunable_threshold=30K, total=200K:
	// Protected ≈ 10 newest messages (50K tokens), Prunable ≈ 150K tokens → exceeds 30K.
	_, maskedCount := MaskOldObservationsBudget(messages, 50_000, 30_000)
	if maskedCount == 0 {
		t.Error("expected some results to be masked")
	}
}

func TestMaskOldObservationsBudget_ExemptTools(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "t1",
				Name: "AskUserQuestion", Content: strings.Repeat("x", 50000)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, ToolUseID: "t2",
				Name: "bash", Content: strings.Repeat("y", 50000)},
		}},
	}

	_, maskedCount := MaskOldObservationsBudget(messages, 10_000, 5_000)
	// AskUserQuestion should be exempt, only bash can be masked.
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Name == "AskUserQuestion" && block.Content == maskedPlaceholder {
				t.Error("AskUserQuestion should be exempt from masking")
			}
		}
	}
	_ = maskedCount
}

func TestMaskOldObservationsBudget_BelowBatchThreshold(t *testing.T) {
	// Small tool results — total prunable should be below threshold.
	var messages []ConversationMessage
	for i := range 5 {
		messages = append(messages, ConversationMessage{
			Role: RoleUser,
			Content: []ContentBlock{
				{Type: ContentTypeToolResult, ToolUseID: "t" + strings.Repeat("0", i),
					Name: "bash", Content: "small result"},
			},
		})
	}

	_, maskedCount := MaskOldObservationsBudget(messages, 50_000, 30_000)
	if maskedCount != 0 {
		t.Errorf("expected 0 masked (below batch threshold), got %d", maskedCount)
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
