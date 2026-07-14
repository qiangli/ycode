package conversation

import (
	"strings"
	"testing"
)

// A CAPPED DELEGATE IS INTERRUPTED, NOT EMPTY-HANDED.
//
// The spawner used to `return "", err` when a subagent hit its iteration cap — throwing
// away every finding from every one of its turns and handing the parent an error it
// could not distinguish from "my delegate found nothing".
//
// That is the absence-of-evidence bug living inside the delegation path, and it cost a
// real run: a conductor delegated correctly, its subagents were killed at the cap, it got
// back errors carrying ZERO information, had nothing to reason from, announced a fallback
// and stopped. 25 turns, 169 tool calls, not one finding. The MODEL was blamed. It was
// our cap.
func TestPartialReportCarriesTheWorkAndSaysItIsPartial(t *testing.T) {
	progress := []string{
		"RouteContent deletes the middle of a read_file result over 2000 chars.",
		"It is called unconditionally from distillResults.",
	}
	got := partialSubagentReport(progress, map[string]bool{"grep_search": true, "read_file": true}, 15)

	// The work survives. This is the whole fix.
	for _, finding := range progress {
		if !strings.Contains(got, finding) {
			t.Errorf("a finding the subagent established was discarded: %q", finding)
		}
	}

	// And the parent is told what the result MEANS.
	for _, must := range []string{"PARTIAL", "did NOT finish", "15-iteration"} {
		if !strings.Contains(got, must) {
			t.Errorf("the report does not say it is partial (missing %q):\n%s", must, got)
		}
	}

	// The one inference the parent must NOT draw.
	if !strings.Contains(got, "is NOT evidence that there is nothing to find") {
		t.Error("the report does not warn against concluding from the absence of a finding — " +
			"which is exactly the conclusion the old error forced")
	}

	// It says what to DO. A warning with no next step just relocates the dead end.
	if !strings.Contains(got, "re-delegate") {
		t.Error("the report does not tell the parent how to recover")
	}
}

// A subagent that produced nothing must still SAY so, plainly, rather than return an
// empty string that reads as a completed investigation with no findings.
func TestPartialReportWithNoFindingsIsExplicit(t *testing.T) {
	got := partialSubagentReport(nil, nil, 15)

	if !strings.Contains(got, "no findings") {
		t.Errorf("a subagent that established nothing did not say so:\n%s", got)
	}
	if !strings.Contains(got, "PARTIAL") {
		t.Error("an empty partial result did not identify itself as partial — the parent " +
			"would read it as a finished investigation that found nothing")
	}
}
