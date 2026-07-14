package session

import (
	"strings"
	"testing"
)

func assistantWithUsage(in, out int) ConversationMessage {
	return ConversationMessage{
		Role:    RoleAssistant,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "ok"}},
		Usage:   &TokenUsage{InputTokens: in, OutputTokens: out},
	}
}

func toolResult(chars int) ConversationMessage {
	return ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeToolResult, Content: strings.Repeat("x", chars)}},
	}
}

// The provider's number is used, not our guess.
func TestMeasureTokensPrefersTheProvidersCount(t *testing.T) {
	msgs := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: strings.Repeat("x", 400_000)}}},
		assistantWithUsage(90_000, 500),
	}
	used := MeasureTokens(msgs)

	if !used.HasReport {
		t.Fatal("a conversation with a counted assistant turn reported no provider count")
	}
	if used.Reported != 90_500 {
		t.Errorf("Reported = %d, want 90500 (the provider's own number)", used.Reported)
	}
	if used.Unreported != 0 {
		t.Errorf("Unreported = %d, want 0 — nothing has been appended since the count", used.Unreported)
	}
}

// THE LAG — codex's first objection, and the one that could have blown the window.
//
// The provider's count describes the PREVIOUS request. Between that response and the
// next request we append tool results, which can be enormous. Gating on the reported
// number alone would say "90K, plenty of room" while the request we are about to send
// is actually 140K.
//
// So the reported count is the BASE and the tail is ESTIMATED. The estimator survives
// exactly here: confined to one turn's additions, sitting on a number that is exact.
func TestMeasureTokensCountsWhatWasAppendedAfterTheLastReport(t *testing.T) {
	const dumpChars = 200_000 // ~50k tokens, well under the 256KB safety cap

	msgs := []ConversationMessage{
		assistantWithUsage(90_000, 0),
		toolResult(dumpChars),
	}
	used := MeasureTokens(msgs)

	if used.Reported != 90_000 {
		t.Fatalf("Reported = %d, want 90000", used.Reported)
	}
	if used.Unreported < 40_000 {
		t.Errorf("Unreported = %d — a %d-char tool result appended since the last count "+
			"was not measured; the next request would silently overflow", used.Unreported, dumpChars)
	}

	// 128K model, no caching. The request is ~140K. It MUST be caught.
	b := ContextBudgetForProvider(128_000, false)
	if !b.NeedsCompaction(used) {
		t.Errorf("a ~%dk-token request against a 128k window was not flagged for compaction "+
			"— this is the one-turn lag, and it is how the window gets blown", used.Total()/1000)
	}
}

// THE RESUME HOLE — codex's third objection.
//
// A session loaded from disk arrives with a full history and no in-memory usage. If
// "no reported count" meant "nothing to compact", the first request after a resume
// would sail straight past the window: a success state reached by the ABSENCE of a
// measurement, which is the exact bug class this whole change exists to kill.
//
// Usage is persisted per message, so the count comes back with the history.
func TestMeasureTokensSurvivesAResumedSession(t *testing.T) {
	// Exactly what Load() returns: messages off disk, usage included.
	loaded := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "old work"}}},
		assistantWithUsage(95_000, 1_000),
	}
	used := MeasureTokens(loaded)

	if !used.HasReport {
		t.Fatal("a resumed session lost its token count — the first request would be unmeasured")
	}
	b := ContextBudgetForProvider(128_000, false)
	if !b.NeedsCompaction(used) {
		t.Errorf("a resumed 96k-token session against a 128k window was not flagged for "+
			"compaction on its first request (total=%d, compact_at=%d)", used.Total(), b.CompactionThreshold)
	}
}

// A brand-new conversation has nothing to compact, and that is the truth, not a fallback.
func TestFreshConversationNeedsNothing(t *testing.T) {
	msgs := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "implement Wrap"}}},
	}
	used := MeasureTokens(msgs)
	b := ContextBudgetForProvider(128_000, false)

	if used.HasReport {
		t.Error("a conversation with no assistant turn claimed a provider count")
	}
	if b.NeedsCompaction(used) {
		t.Error("a one-message conversation was flagged for compaction")
	}
	if b.NeedsTrim(used) {
		t.Error("a one-message conversation was flagged for trimming — this is the turn-1 bug")
	}
}

// The cheap rung fires BEFORE the expensive one. Without it, every pressure event on a
// long non-caching session pays for an LLM summarization call.
func TestTrimComesBeforeCompact(t *testing.T) {
	b := ContextBudgetForProvider(128_000, false)
	if b.SoftTrimAt() >= b.CompactionThreshold {
		t.Fatalf("trim (%d) must fire below compaction (%d), or the cheap rung is unreachable",
			b.SoftTrimAt(), b.CompactionThreshold)
	}

	// Right between the two: trim, don't compact.
	used := TokensUsed{Reported: (b.SoftTrimAt() + b.CompactionThreshold) / 2, HasReport: true}
	if !b.NeedsTrim(used) {
		t.Error("a conversation past the trim line was not trimmed")
	}
	if b.NeedsCompaction(used) {
		t.Error("a conversation below the compaction line paid for an LLM summarization anyway")
	}
}
