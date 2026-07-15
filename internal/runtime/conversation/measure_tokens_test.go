package conversation

import (
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/session"
)

// A durable message with ~n tokens of text (EstimateMessageTokens ≈ chars/4).
func msgOf(n int) session.ConversationMessage {
	return session.ConversationMessage{
		Role:    session.MessageRole("user"),
		Content: []session.ContentBlock{{Type: session.ContentType("text"), Text: strings.Repeat("x", n*4)}},
	}
}

// The bug this reproduces, from a live run: GLM reported the request at 16k tokens, but
// ycode estimated a 230k "unreported" tail and reported total=246k — tripping the 56k
// compaction threshold EVERY turn. It compacted 119 times, threw away the files the agent
// had just read, and the agent re-read them and never converged (40 min, 0 files written).
//
// Root cause: the tail baseline (lastReportedAtMsgs) was recorded from the COMPACTED
// send-list (short), but measureTokens estimates the tail against the DURABLE history
// (long). msgs[shortIndex:longLen] is a phantom tail. The fix anchors the baseline to the
// durable list in TurnWithRecovery.
func TestMeasureTokensDoesNotInventAPhantomTail(t *testing.T) {
	// A long durable history (200 messages, ~1k tokens each ≈ 200k of history).
	durable := make([]session.ConversationMessage, 200)
	for i := range durable {
		durable[i] = msgOf(1000)
	}

	// CORRECT (post-fix): the baseline is anchored to the durable length. The provider
	// already accounted for the whole request; the tail is only what came after it.
	r := &Runtime{lastReportedTokens: 16000, lastReportedAtMsgs: len(durable) + 1}
	used := r.measureTokens(durable)
	if !used.HasReport {
		t.Fatal("HasReport should be true when the provider reported a count")
	}
	if used.Unreported != 0 {
		t.Errorf("unreported=%d, want 0 — baseline anchored at/after the durable end must "+
			"not estimate a tail", used.Unreported)
	}
	if used.Total() != 16000 {
		t.Errorf("total=%d, want 16000 (the provider's count) — anything larger is the "+
			"phantom tail that forced compaction every turn", used.Total())
	}

	// THE BUG SHAPE (pre-fix): a baseline recorded from a compacted send-list of 20 while
	// the durable history is 200. This is what produced the 230k phantom tail. We assert it
	// HERE so the fix is pinned by contrast: a short/stale baseline over-counts massively.
	buggy := &Runtime{lastReportedTokens: 16000, lastReportedAtMsgs: 20}
	buggyUsed := buggy.measureTokens(durable)
	if buggyUsed.Total() <= 16000 {
		t.Fatal("expected the stale-baseline case to demonstrate the over-count; if it no " +
			"longer does, this contrast test is stale")
	}
	// The phantom tail is enormous — ~180 messages * 1k ≈ 180k — dwarfing the real 16k.
	if buggyUsed.Unreported < 100000 {
		t.Errorf("stale baseline tail=%d; expected a large phantom tail (~180k) — this is "+
			"the failure the fix prevents by anchoring to the durable list", buggyUsed.Unreported)
	}
}
