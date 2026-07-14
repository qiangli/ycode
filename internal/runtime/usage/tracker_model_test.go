package usage

import "testing"

// EVERY SESSION WAS PRICED AS CLAUDE SONNET.
//
// Tracker.Cost() gated on `if t.Model != ""` and fell through to a THIRD hardcoded copy of
// Claude's price ($3/$15 per million) — one that did not even read PricingTable. And
// t.Model was ALWAYS empty, because every production call site used Add(), which never set
// it, rather than AddWithModel().
//
// So the "Est. cost: $X" line at the end of every run reported every model on earth as
// Claude Sonnet. GLM-5.2 read 5.5x high.
//
// Found by an adversarial review (glm-5.2) — in code I had JUST fixed the metric path and
// the pricing table for, and written a commit message about. It found the copy I missed.
func TestSessionCostIsPricedAgainstTheModelThatActuallyRan(t *testing.T) {
	const in, out = 1_000_000, 100_000

	glm := NewTracker()
	glm.AddWithModel("glm-5.2", in, out, 0, 0)

	claude := NewTracker()
	claude.AddWithModel("claude-sonnet-4-6", in, out, 0, 0)

	gc, cc := glm.Cost(), claude.Cost()
	if gc >= cc {
		t.Fatalf("glm-5.2 session cost $%.4f >= claude-sonnet $%.4f — glm is one of the "+
			"CHEAPEST models in the fleet and is being priced like the dearest", gc, cc)
	}
	t.Logf("1M in / 100k out — glm-5.2: $%.2f   claude-sonnet: $%.2f   (%.1fx)", gc, cc, cc/gc)
}

// A cost with no model must ADMIT it is a guess, rather than quietly reporting a
// confident, specific, wrong number.
func TestACostWithNoModelSaysItIsAGuess(t *testing.T) {
	tr := NewTracker()
	tr.Add(1000, 100, 0, 0) // the deprecated path: no model

	if !tr.CostIsGuess() {
		t.Error("a session priced with no model reported CostIsGuess()=false — the guess is " +
			"masquerading as a measurement, which is the whole bug")
	}

	known := NewTracker()
	known.AddWithModel("glm-5.2", 1000, 100, 0, 0)
	if known.CostIsGuess() {
		t.Error("a session priced against a DECLARED rate was reported as a guess")
	}
}
