package conversation

import "testing"

// The core contract: N exploration-only turns are allowed, the next one
// triggers exactly one grace instruction, and any tool turn after that stops
// the run.
func TestConvergencePolicy_GraceThenHardStop(t *testing.T) {
	p := NewConvergencePolicy(3)

	for i := 0; i < 3; i++ {
		if got := p.ObserveToolTurn(false); got != ConvergeContinue {
			t.Fatalf("exploration turn %d: got %v, want ConvergeContinue", i+1, got)
		}
	}
	if got := p.ObserveToolTurn(false); got != ConvergeGrace {
		t.Fatalf("budget-exhausting turn: got %v, want ConvergeGrace", got)
	}
	// Tool use continued after grace — stop, and keep stopping.
	if got := p.ObserveToolTurn(false); got != ConvergeStop {
		t.Fatalf("post-grace tool turn: got %v, want ConvergeStop", got)
	}
	if got := p.ObserveToolTurn(true); got != ConvergeStop {
		t.Fatalf("post-grace tool turn with progress: got %v, want ConvergeStop (grace is final)", got)
	}
}

// Progress resets the budget: a long productive run alternating exploration
// and writes never reaches grace.
func TestConvergencePolicy_ProgressResetsBudget(t *testing.T) {
	p := NewConvergencePolicy(2)

	for cycle := 0; cycle < 10; cycle++ {
		if got := p.ObserveToolTurn(false); got != ConvergeContinue {
			t.Fatalf("cycle %d explore 1: got %v", cycle, got)
		}
		if got := p.ObserveToolTurn(false); got != ConvergeContinue {
			t.Fatalf("cycle %d explore 2: got %v", cycle, got)
		}
		if got := p.ObserveToolTurn(true); got != ConvergeContinue {
			t.Fatalf("cycle %d progress: got %v", cycle, got)
		}
		if n := p.NoProgressTurns(); n != 0 {
			t.Fatalf("cycle %d: NoProgressTurns = %d after progress, want 0", cycle, n)
		}
	}
}

func TestConvergencePolicy_NilIsInert(t *testing.T) {
	var p *ConvergencePolicy
	for i := 0; i < 500; i++ {
		if got := p.ObserveToolTurn(false); got != ConvergeContinue {
			t.Fatalf("nil policy returned %v", got)
		}
	}
	if p.NoProgressTurns() != 0 || p.Budget() != 0 {
		t.Fatal("nil policy accessors must return zero")
	}
}

func TestConvergencePolicy_ClampsNonPositiveBudget(t *testing.T) {
	p := NewConvergencePolicy(0)
	if p.Budget() != 1 {
		t.Fatalf("Budget = %d, want 1 (clamped)", p.Budget())
	}
	if got := p.ObserveToolTurn(false); got != ConvergeContinue {
		t.Fatalf("first exploration turn: got %v", got)
	}
	if got := p.ObserveToolTurn(false); got != ConvergeGrace {
		t.Fatalf("second exploration turn: got %v, want ConvergeGrace", got)
	}
}
