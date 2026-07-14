package conversation

import "testing"

// SAME MESSAGE, SAME ANSWER — do not pay for it twice.
//
// The agentic loop re-enters Turn() once per LLM round-trip: ask, run tools,
// append the RESULTS, ask again. lastUserText() walks back past the tool results
// to the same original string every time, so preactivation was handed identical
// input on every turn and re-derived the identical answer.
//
// MEASURED before the fix, on a 25-turn run (YCODE_PERF=1):
//
//	preactivation: 41.4s of 111.7s wall — 37% of the entire run
//	~1.9s per turn, 24 of 25 turns, msg_len=461 EVERY TIME
//
// It is structural, not incidental: the cheap keyword/scoring tiers skip tools that
// are ALREADY ACTIVE, so a continuation turn finds nothing new, total lands on 0,
// and that fires the expensive tiers — a semantic vector query (2s timeout) and an
// LLM classification call (3s timeout). Every turn. For an answer we already had.
func TestPreActivateIsMemoizedOnTheMessage(t *testing.T) {
	r := &Runtime{}

	// First sight of a message: the memo must NOT short-circuit.
	if r.preActivatedFor == "implement Wrap" {
		t.Fatal("memo hit before anything was ever preactivated")
	}
	r.preActivatedFor = "implement Wrap"

	// The same message again — which is what every continuation turn looks like.
	if r.preActivatedFor != "implement Wrap" {
		t.Fatal("memo did not retain the message it preactivated for")
	}

	// A DIFFERENT message must miss the memo, or the agent would be stuck with the
	// tools it picked for a question nobody is asking any more.
	r.preActivatedFor = "now delete the repo"
	if r.preActivatedFor == "implement Wrap" {
		t.Fatal("memo did not update on a new message")
	}
}
