package session

import (
	"strings"
	"testing"
)

// BELOW PRESSURE, A TOOL RESULT IS DELIVERED VERBATIM.
//
// The first version of the pressure gate skipped RouteContent and then still ran
// DistillToolOutput — which head/tails at MaxInlineChars, and that is 1000 chars
// for a non-caching provider. So a 2.4KB read_file was STILL being mutilated below
// pressure, by the second of two functions doing the same thing.
//
// The measurement had improved enough to hide it (85s -> 42.6s, no more xxd). An
// adversarial review caught what the numbers did not. The principle has to apply to
// both paths or it is not a principle.
func TestCapToolOutputLeavesOrdinaryOutputAlone(t *testing.T) {
	// The file that started all of this: ~2.4KB. It must arrive whole.
	content := strings.Repeat("line of a test file that is the specification\n", 50)
	if len(content) < 2000 {
		t.Fatalf("test fixture too small to exercise the old thresholds: %d", len(content))
	}
	if got := CapToolOutput(content); got != content {
		t.Errorf("a %d-byte tool result was altered below pressure — this is what deleted "+
			"the middle of the spec and cost seventeen turns of base64 and xxd", len(content))
	}
}

// The one limit that survives: a pathological output must not be inlined whatever
// the window looks like. That is a safety valve, not context management.
func TestCapToolOutputStillStopsAPathologicalDump(t *testing.T) {
	huge := strings.Repeat("x", AbsoluteToolOutputCap+5000)
	got := CapToolOutput(huge)
	if len(got) > AbsoluteToolOutputCap+300 {
		t.Errorf("a %d-byte output was inlined whole: %d", len(huge), len(got))
	}
	if !strings.Contains(got, "safety cap") {
		t.Error("a truncated output must SAY it was truncated — silence here is the bug " +
			"this whole change exists to remove")
	}
}
