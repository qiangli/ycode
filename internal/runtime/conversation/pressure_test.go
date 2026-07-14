package conversation

import (
	"testing"

	"github.com/qiangli/ycode/internal/runtime/session"
)

// DO NOT MANAGE A CONTEXT PROBLEM YOU DO NOT HAVE.
//
// Content routing exists to survive a full window. It was running UNCONDITIONALLY:
// on turn 1 of an empty conversation, against a 64K-token window with ~600 tokens
// used, a read_file result over 2000 characters was classified RoutePartial —
// which keeps the head and the tail and DELETES THE MIDDLE.
//
// Measured on the wire, on a real task: the model asked to read the 2.4KB test file
// it had been told to implement against, and got it back with the TEST CASES cut
// out of the middle. It then spent SEVENTEEN turns trying to recover them — cat,
// sed ranges, python, awk, base64, and finally xxd to hexdump the file. It was not
// confused. It was doing exactly what anyone would do when handed a document with
// the middle torn out.
//
//	before: 23 turns, 22 tool calls, 16 of them bash, ~85s, gate 2/3
//	after:  10-12 tool calls, 3-6 bash, ~42s, gate 3/3
func TestNoContextManagementWithoutContextPressure(t *testing.T) {
	// The budget of the model actually used in the benchmark: 128K window, no prompt
	// caching. NOT the zero value — see TestUnsetBudgetIsNotAFullWindow.
	r := &Runtime{contextBudget: session.ContextBudgetForProvider(128_000, false)}
	soft := r.contextBudget.SoftTrimAt()

	// An empty conversation. There is no context problem to solve.
	r.lastContextChars = 0
	if r.contextUnderPressure() {
		t.Fatal("an EMPTY conversation reported context pressure — this is what deleted " +
			"the middle of a 2KB file on turn 1 and cost seventeen turns of xxd")
	}

	// A realistic mid-task conversation. Still far short of the soft threshold.
	r.lastContextChars = 25_000
	if r.contextUnderPressure() {
		t.Errorf("25KB of context (~6k tokens) reported as pressure against a soft threshold of %d tokens", soft)
	}

	// Genuinely large: past the soft threshold. NOW routing should engage, because
	// now the trade is real. The point is to trim when trimming is WARRANTED — not
	// to never trim. A gate that never fires is as broken as one that always does.
	r.lastContextChars = (soft + 1_000) * 4
	if !r.contextUnderPressure() {
		t.Errorf("a conversation past the soft threshold (%d tokens) must engage content routing", soft)
	}
}

// TestUnsetBudgetIsNotAFullWindow pins the fail-safe.
//
// A zero-value ContextBudget has CompactionThreshold 0, so SoftTrimAt() is 0, and
// `chars/4 >= 0` is true for EVERY conversation including an empty one. A missing
// budget would therefore switch content damage back on for everything — reintroducing
// the exact bug this gate exists to prevent, and doing it silently.
//
// Damaging the model's observations is the aggressive act. It must never be reached
// by the ABSENCE of a number.
func TestUnsetBudgetIsNotAFullWindow(t *testing.T) {
	r := &Runtime{} // no budget at all
	r.lastContextChars = 0
	if r.contextUnderPressure() {
		t.Fatal("a Runtime with NO context budget reported an empty conversation as under pressure — " +
			"an unset budget is being read as a full window")
	}
}
