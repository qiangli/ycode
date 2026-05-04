package sprint

import (
	"sort"
	"testing"
)

func TestDepGraph_BasicDependency(t *testing.T) {
	g := NewDepGraph()
	if err := g.AddDep("B", "A"); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	if !g.IsBlocked("B") {
		t.Error("B should be blocked by A")
	}
	if g.IsBlocked("A") {
		t.Error("A should not be blocked")
	}
}

func TestDepGraph_AutoUnblock(t *testing.T) {
	g := NewDepGraph()
	g.AddDep("B", "A")
	g.AddDep("C", "A")

	unblocked := g.MarkComplete("A")
	sort.Strings(unblocked)

	if len(unblocked) != 2 {
		t.Fatalf("unblocked count = %d, want 2", len(unblocked))
	}
	if unblocked[0] != "B" || unblocked[1] != "C" {
		t.Errorf("unblocked = %v, want [B C]", unblocked)
	}
	if g.IsBlocked("B") || g.IsBlocked("C") {
		t.Error("B and C should no longer be blocked")
	}
}

func TestDepGraph_PartialUnblock(t *testing.T) {
	g := NewDepGraph()
	g.AddDep("C", "A")
	g.AddDep("C", "B")

	unblocked := g.MarkComplete("A")
	if len(unblocked) != 0 {
		t.Errorf("C should still be blocked by B, unblocked = %v", unblocked)
	}
	if !g.IsBlocked("C") {
		t.Error("C should still be blocked")
	}

	unblocked = g.MarkComplete("B")
	if len(unblocked) != 1 || unblocked[0] != "C" {
		t.Errorf("C should now be unblocked, got %v", unblocked)
	}
}

func TestDepGraph_CycleDetection(t *testing.T) {
	g := NewDepGraph()
	g.AddDep("B", "A")
	g.AddDep("C", "B")

	err := g.AddDep("A", "C")
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestDepGraph_SelfDependency(t *testing.T) {
	g := NewDepGraph()
	err := g.AddDep("A", "A")
	if err == nil {
		t.Error("expected self-dependency error")
	}
}

func TestDepGraph_BlockedBy(t *testing.T) {
	g := NewDepGraph()
	g.AddDep("C", "A")
	g.AddDep("C", "B")

	deps := g.BlockedBy("C")
	sort.Strings(deps)
	if len(deps) != 2 || deps[0] != "A" || deps[1] != "B" {
		t.Errorf("BlockedBy(C) = %v, want [A B]", deps)
	}
}

func TestDepGraph_Dependents(t *testing.T) {
	g := NewDepGraph()
	g.AddDep("B", "A")
	g.AddDep("C", "A")

	deps := g.Dependents("A")
	sort.Strings(deps)
	if len(deps) != 2 || deps[0] != "B" || deps[1] != "C" {
		t.Errorf("Dependents(A) = %v, want [B C]", deps)
	}
}

func TestDepGraph_ReadyTasks(t *testing.T) {
	g := NewDepGraph()
	g.AddDep("B", "A")
	g.AddDep("D", "C")

	candidates := []string{"A", "B", "C", "D"}
	ready := g.ReadyTasks(candidates)
	sort.Strings(ready)

	if len(ready) != 2 || ready[0] != "A" || ready[1] != "C" {
		t.Errorf("ReadyTasks = %v, want [A C]", ready)
	}
}

func TestDepGraph_WavePattern(t *testing.T) {
	// Simulate ClawTeam wave pattern: wave 2 blocked by all wave 1 tasks.
	g := NewDepGraph()
	wave1 := []string{"w1-a", "w1-b"}
	wave2 := []string{"w2-a", "w2-b"}

	for _, w2 := range wave2 {
		for _, w1 := range wave1 {
			g.AddDep(w2, w1)
		}
	}

	// Complete wave 1 task A — wave 2 still blocked.
	unblocked := g.MarkComplete("w1-a")
	if len(unblocked) != 0 {
		t.Errorf("wave 2 should still be blocked, got %v", unblocked)
	}

	// Complete wave 1 task B — wave 2 unblocked.
	unblocked = g.MarkComplete("w1-b")
	sort.Strings(unblocked)
	if len(unblocked) != 2 {
		t.Fatalf("wave 2 should be unblocked, got %v", unblocked)
	}
}

func TestDepGraph_EmptyGraph(t *testing.T) {
	g := NewDepGraph()

	if g.IsBlocked("nonexistent") {
		t.Error("nonexistent task should not be blocked")
	}
	unblocked := g.MarkComplete("nonexistent")
	if len(unblocked) != 0 {
		t.Errorf("should not unblock anything, got %v", unblocked)
	}
}
