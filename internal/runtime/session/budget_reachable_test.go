package session

import "testing"

// TestEveryThresholdIsReachable is a ratchet, not a unit test.
//
// A context-safety threshold that sits ABOVE the model's usable window is not a
// conservative setting. It is dead code. It can never fire, the conversation runs
// until the API rejects it, and every log line and dashboard says the context is
// "healthy" right up to the error.
//
// That is exactly what shipped. Every consumer of this package divided by the
// package-level CompactionThreshold — a flat 100_000 — regardless of the model on
// the other end. On a 64K model the usable window is 48K tokens, so:
//
//	soft trim   60_000  (125% of usable)  never fired
//	hard clear  80_000  (167% of usable)  never fired
//	compaction 100_000  (208% of usable)  never fired
//
// ContextBudgetForProvider had computed the right numbers all along (24K/14.4K/19.2K)
// and stored them on the Runtime. Nothing read them.
//
// This test fails the build if that is ever true again, for any window we support.
func TestEveryThresholdIsReachable(t *testing.T) {
	windows := []int{8_000, 16_000, 32_000, 64_000, 128_000, 200_000, 1_000_000}

	for _, window := range windows {
		for _, caching := range []bool{true, false} {
			b := ContextBudgetForProvider(window, caching)
			usable := b.EffectiveMax()

			if usable <= 0 {
				t.Errorf("window=%d caching=%v: usable window is %d — the reserve eats the whole context",
					window, caching, usable)
				continue
			}

			// The order that makes these mechanisms mean anything: you trim before you
			// clear, you clear before you compact, and you compact before you overflow.
			checks := []struct {
				name string
				at   int
			}{
				{"soft trim", b.SoftTrimAt()},
				{"hard clear", b.HardClearAt()},
				{"compaction", b.CompactionThreshold},
			}
			for _, c := range checks {
				if c.at >= usable {
					t.Errorf("window=%d caching=%v: %s fires at %d tokens but only %d are usable — it can NEVER fire, and the conversation will overflow instead",
						window, caching, c.name, c.at, usable)
				}
			}

			if b.SoftTrimAt() >= b.HardClearAt() {
				t.Errorf("window=%d caching=%v: soft trim (%d) is not before hard clear (%d)",
					window, caching, b.SoftTrimAt(), b.HardClearAt())
			}
			if b.HardClearAt() >= b.CompactionThreshold {
				t.Errorf("window=%d caching=%v: hard clear (%d) is not before compaction (%d)",
					window, caching, b.HardClearAt(), b.CompactionThreshold)
			}
		}
	}
}

// TestTheReserveCoversTheResponse pins the invariant that the reserve exists FOR.
//
// The reserve is not a property of the model, it is a property of the request: it
// holds the reply we are about to ask for. It was not tied to it. On a 128K window
// the table reserved 30K while the runtime asked for up to 32K of output — so a full
// conversation plus its own reply exceeded the window, and the reserve was short by
// precisely the thing it was reserving for.
func TestTheReserveCoversTheResponse(t *testing.T) {
	const maxOutput = 32_000 // MaxOutputTokenCap

	for _, window := range []int{8_000, 32_000, 64_000, 128_000, 200_000, 1_000_000} {
		for _, caching := range []bool{true, false} {
			base := ContextBudgetForProvider(window, caching)

			// What we ACTUALLY ask for — not the flat ceiling. A 32K model cannot be
			// asked for a 32K reply; that is the whole window.
			ask := base.MaxResponseTokens(maxOutput)
			b := base.WithResponseReserve(ask)

			// The whole conversation plus the whole reply must fit in the window.
			if got := b.EffectiveMax() + ask; got > b.ContextWindow {
				t.Errorf("window=%d caching=%v: a full conversation (%d) plus the reply we ask for (%d) is %d — it does not fit in %d",
					window, caching, b.EffectiveMax(), ask, got, b.ContextWindow)
			}
			if ask > b.ReservedTokens {
				t.Errorf("window=%d caching=%v: we ask for %d output tokens but only reserve %d — the reserve is a decoration",
					window, caching, ask, b.ReservedTokens)
			}
			if b.CompactionThreshold >= b.EffectiveMax() {
				t.Errorf("window=%d caching=%v: compaction (%d) is still outside the usable window (%d) after reserving for the response",
					window, caching, b.CompactionThreshold, b.EffectiveMax())
			}
		}
	}
}

// TestUnknownModelIsAssumedSmall — an unknown model is the one case where guessing
// high is unrecoverable. Guess low and you pay for some avoidable compaction; guess
// high and every request fails.
func TestUnknownModelIsAssumedSmall(t *testing.T) {
	b := DefaultContextBudget()
	if b.ContextWindow > 32_000 {
		t.Errorf("unknown model assumed a %d-token window — that is a guess, and guessing BIG is the one that breaks", b.ContextWindow)
	}
	if b.CompactionThreshold >= b.EffectiveMax() {
		t.Errorf("the default budget's own compaction threshold (%d) is outside its usable window (%d)",
			b.CompactionThreshold, b.EffectiveMax())
	}
}

// TestTheGlobalConstantIsNotABudget documents WHY the constant may not be used as a
// threshold, using the exact model that exposed it.
//
// It is kept as the default for a budget nobody configured. It is not a budget.
func TestTheGlobalConstantIsNotABudget(t *testing.T) {
	const deepseekWindow = 64_000
	b := ContextBudgetForProvider(deepseekWindow, true)

	if CompactionThreshold < b.EffectiveMax() {
		t.Fatalf("this test has gone stale: the global constant (%d) no longer exceeds "+
			"the usable window of a %d-token model (%d), so it no longer demonstrates the bug",
			CompactionThreshold, deepseekWindow, b.EffectiveMax())
	}

	// The point: using the constant here would put the trigger past the cliff.
	if got := b.CompactionThreshold; got >= CompactionThreshold {
		t.Fatalf("per-model compaction threshold (%d) should be well under the global default (%d)",
			got, CompactionThreshold)
	}
}
