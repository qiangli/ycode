package autoloop

import "testing"

func TestStallDetector_NoStall(t *testing.T) {
	d := NewStallDetector(3, 2)

	// Improving scores should never trigger.
	for i := range 5 {
		action := d.Observe(float64(i))
		if action != StallContinue {
			t.Errorf("iteration %d: expected Continue, got %d", i, action)
		}
	}
}

func TestStallDetector_TriggersReplan(t *testing.T) {
	d := NewStallDetector(3, 2)

	d.Observe(50.0) // initial

	// 3 consecutive stalls.
	d.Observe(50.0)           // stall 1
	d.Observe(50.0)           // stall 2
	action := d.Observe(50.0) // stall 3 → replan

	if action != StallReplan {
		t.Errorf("expected StallReplan, got %d", action)
	}

	stats := d.Stats()
	if stats.ReplanCount != 1 {
		t.Errorf("replan count = %d, want 1", stats.ReplanCount)
	}
}

func TestStallDetector_AbortsAfterMaxReplans(t *testing.T) {
	d := NewStallDetector(2, 2)

	d.Observe(50.0) // initial

	// First replan.
	d.Observe(50.0)
	action := d.Observe(50.0)
	if action != StallReplan {
		t.Fatalf("expected first replan, got %d", action)
	}

	// Second replan.
	d.Observe(50.0)
	action = d.Observe(50.0)
	if action != StallReplan {
		t.Fatalf("expected second replan, got %d", action)
	}

	// Third stall cycle → abort.
	d.Observe(50.0)
	action = d.Observe(50.0)
	if action != StallAbort {
		t.Errorf("expected StallAbort, got %d", action)
	}
}

func TestStallDetector_ProgressResetsCount(t *testing.T) {
	d := NewStallDetector(3, 2)

	d.Observe(50.0)
	d.Observe(50.0) // stall 1
	d.Observe(50.0) // stall 2

	// Progress resets stall count.
	d.Observe(60.0)

	// Need 3 more stalls before replan.
	d.Observe(60.0)           // stall 1
	action := d.Observe(60.0) // stall 2
	if action != StallContinue {
		t.Errorf("expected Continue (only 2 stalls), got %d", action)
	}

	action = d.Observe(60.0) // stall 3 → replan
	if action != StallReplan {
		t.Errorf("expected StallReplan, got %d", action)
	}
}

func TestStallDetector_Reset(t *testing.T) {
	d := NewStallDetector(2, 1)

	d.Observe(50.0)
	d.Observe(50.0)
	d.Observe(50.0) // triggers replan

	d.Reset()

	stats := d.Stats()
	if stats.ReplanCount != 0 {
		t.Errorf("after reset: replan count = %d, want 0", stats.ReplanCount)
	}
	if stats.ConsecutiveStalls != 0 {
		t.Errorf("after reset: stalls = %d, want 0", stats.ConsecutiveStalls)
	}

	// Should start fresh.
	action := d.Observe(50.0)
	if action != StallContinue {
		t.Errorf("after reset: expected Continue on first observe, got %d", action)
	}
}

func TestNewStallDetector_Defaults(t *testing.T) {
	d := NewStallDetector(0, 0)
	if d.MaxStalls != 3 {
		t.Errorf("default MaxStalls = %d, want 3", d.MaxStalls)
	}
	if d.MaxReplans != 2 {
		t.Errorf("default MaxReplans = %d, want 2", d.MaxReplans)
	}
}
