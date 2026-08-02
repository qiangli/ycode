package prompt

import (
	"strings"
	"testing"
)

// TestKnowledgeSection_EmptyIsInert — the control arm of the A/B lives or dies
// here. With BASHY_KNOWLEDGE unset the field is empty, and an empty field must
// produce NO section at all: not a header, not a "nothing known" line. Anything
// rendered is a difference between the arms that is not the treatment, and it
// would silently confound every measurement taken with this build.
func TestKnowledgeSection_EmptyIsInert(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t "} {
		if got := KnowledgeSection(in); got != "" {
			t.Errorf("KnowledgeSection(%q) = %q, want empty — arm B must be byte-identical", in, got)
		}
	}
}

// TestKnowledgeSection_IsCitedNotInstructed — a recalled `candidate` note asserted
// as policy becomes a constraint the agent cannot argue with. The block has to say
// what it is.
func TestKnowledgeSection_IsCitedNotInstructed(t *testing.T) {
	got := KnowledgeSection("- [host/validated] never pkill on a paired host  (kb:never-pkill)\n")
	if got == "" {
		t.Fatal("no section rendered for non-empty knowledge")
	}
	low := strings.ToLower(got)
	for _, want := range []string{"may be stale", "not as instruction", "verify"} {
		if !strings.Contains(low, want) {
			t.Errorf("section does not mark itself fallible: missing %q\n%s", want, got)
		}
	}
	if !strings.Contains(got, "kb:never-pkill") {
		t.Error("the citation id was dropped; an agent cannot go read the original")
	}
}

// TestKnowledgeSection_IsDistinctFromMemories — they are different claims from
// different owners. Memories are ycode's own and true of the USER; recalled
// knowledge is the host's shared record, written by other agents, true of the WORK.
// Collapsing them would let a stale host note inherit a memory's authority.
func TestKnowledgeSection_IsDistinctFromMemories(t *testing.T) {
	k := KnowledgeSection("- [host/candidate] something\n")
	if strings.Contains(strings.ToLower(k), "## memor") {
		t.Error("knowledge rendered under the memories heading; the two must stay distinct")
	}
	if !strings.Contains(k, "## Recalled host knowledge") {
		t.Errorf("missing its own heading:\n%s", k)
	}
}

// TestBuildDefault_IncludesRecalledKnowledge proves the WIRING, which the section
// tests above cannot: a formatter nothing calls renders nothing. This is the
// assertion that would have caught the section being written and never added to
// the builder.
func TestBuildDefault_IncludesRecalledKnowledge(t *testing.T) {
	ctx := &ProjectContext{WorkDir: "/tmp/x", Platform: "darwin", Shell: "bash"}

	off := BuildDefault(ctx, "", false, nil)
	if strings.Contains(off, "Recalled host knowledge") {
		t.Error("the knowledge heading appears with no knowledge set — arm B is not clean")
	}

	ctx.RecalledKnowledge = "- [host/validated] a distinctive recalled lesson  (kb:zzz-marker)\n"
	on := BuildDefault(ctx, "", false, nil)
	if !strings.Contains(on, "Recalled host knowledge") {
		t.Fatal("BuildDefault dropped the knowledge section — the builder is not wired")
	}
	if !strings.Contains(on, "kb:zzz-marker") {
		t.Error("the citation did not survive prompt assembly")
	}
}
