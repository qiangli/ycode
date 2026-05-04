package agentpool

import "testing"

func TestCapacityGovernor_Unlimited(t *testing.T) {
	p := New()
	g := NewCapacityGovernor(p, 0)

	if !g.CanSpawn() {
		t.Error("unlimited governor should always allow spawn")
	}
	if g.Remaining() != -1 {
		t.Errorf("remaining = %d, want -1 (unlimited)", g.Remaining())
	}
}

func TestCapacityGovernor_EnforcesLimit(t *testing.T) {
	p := New()
	g := NewCapacityGovernor(p, 2)

	p.Register("a1", "Explore", "agent 1")
	p.SetRunning("a1")

	if !g.CanSpawn() {
		t.Error("should allow spawn when under limit")
	}
	if g.Remaining() != 1 {
		t.Errorf("remaining = %d, want 1", g.Remaining())
	}

	p.Register("a2", "Plan", "agent 2")
	p.SetRunning("a2")

	if g.CanSpawn() {
		t.Error("should deny spawn when at limit")
	}
	if g.Remaining() != 0 {
		t.Errorf("remaining = %d, want 0", g.Remaining())
	}
}

func TestCapacityGovernor_FreesSlotOnComplete(t *testing.T) {
	p := New()
	g := NewCapacityGovernor(p, 1)

	p.Register("a1", "Explore", "agent 1")
	p.SetRunning("a1")

	if g.CanSpawn() {
		t.Error("should deny spawn when at limit")
	}

	p.Complete("a1", false)

	if !g.CanSpawn() {
		t.Error("should allow spawn after agent completes")
	}
}

func TestCapacityGovernor_SpawningCountsAsActive(t *testing.T) {
	p := New()
	g := NewCapacityGovernor(p, 1)

	p.Register("a1", "Explore", "agent 1")
	// Still spawning (not running yet), but counts as active.

	if g.CanSpawn() {
		t.Error("spawning agent should count toward capacity")
	}
}
