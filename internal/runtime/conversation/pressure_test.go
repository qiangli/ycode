package conversation

import "testing"

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
	r := &Runtime{}

	// An empty conversation. There is no context problem to solve.
	r.lastContextChars = 0
	if r.contextUnderPressure() {
		t.Fatal("an EMPTY conversation reported context pressure — this is what deleted " +
			"the middle of a 2KB file on turn 1 and cost seventeen turns of xxd")
	}

	// A realistic mid-task conversation: ~25KB. Still nothing next to a 64K-token
	// window (~256KB of text).
	r.lastContextChars = 25_000
	if r.contextUnderPressure() {
		t.Errorf("25KB of context reported as pressure — the window holds ~10x that")
	}

	// Genuinely large: past the soft threshold. NOW routing should engage, because
	// now the trade is real.
	r.lastContextChars = 60_000 * 4 // 60k tokens x ~4 chars/token
	if !r.contextUnderPressure() {
		t.Error("a conversation past the soft threshold must engage content routing — " +
			"the point is to trim when trimming is WARRANTED, not to never trim")
	}
}
