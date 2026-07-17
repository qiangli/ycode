package api

import "testing"

// TestResolveFleetModelPassthrough — a value that is not a fleet selector is
// returned unchanged, so an ordinary model id can never be mangled.
func TestResolveFleetModelPassthrough(t *testing.T) {
	for _, in := range []string{
		"claude-sonnet-4-6",
		"gpt-4o-mini",
		"some-unregistered-model-xyz",
		"",
	} {
		got, note := ResolveFleetModel(in)
		if got != in {
			t.Errorf("ResolveFleetModel(%q) = %q, want passthrough; note=%q", in, got, note)
		}
	}
}

// TestBandSelectorMatches — the band regex accepts the forms a human types and
// rejects ordinary model ids (which must fall through to passthrough).
func TestBandSelectorMatches(t *testing.T) {
	matches := []string{"L3", "l3", "b2", "B4", "band:3", "band=2", "band 1", "L1"}
	for _, s := range matches {
		if bandRE.FindStringSubmatch(s) == nil {
			t.Errorf("bandRE should match %q", s)
		}
	}
	nonMatches := []string{"gpt-4o", "l", "band", "L", "kimi-k3", "L9x", "3", "bison"}
	for _, s := range nonMatches {
		if bandRE.FindStringSubmatch(s) != nil {
			t.Errorf("bandRE should NOT match %q", s)
		}
	}
}

// TestBandResolvesToYcodeRunnable — a band selector, when the fleet has any
// ycode-runnable model at that band, resolves to a NON-selector concrete id (i.e.
// it changed), and that id is not itself another band token. Tolerant of a fleet
// with no ycode model at a band (then it passes through), so it never flakes on a
// re-pegged catalog.
func TestBandResolvesToYcodeRunnable(t *testing.T) {
	got, note := ResolveFleetModel("L2")
	if got == "L2" {
		t.Skip("no ycode-runnable model at L2 in this fleet — passthrough is correct")
	}
	if bandRE.FindStringSubmatch(got) != nil {
		t.Errorf("resolved band should be a concrete id, got another selector %q", got)
	}
	if note == "" {
		t.Errorf("a resolved selection should carry a note, got empty for %q", got)
	}
}
