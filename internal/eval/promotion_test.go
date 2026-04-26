package eval

import (
	"testing"
)

func TestPromotionAfterConsecutivePasses(t *testing.T) {
	dir := t.TempDir()
	pt, err := NewPromotionTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Record 6 perfect runs — should not promote yet.
	for i := 0; i < ConsecutivePassesRequired-1; i++ {
		event := pt.RecordRun("test_scenario", UsuallyPasses, 1.0)
		if event != nil {
			t.Fatalf("unexpected promotion after %d runs", i+1)
		}
	}

	h := pt.Get("test_scenario")
	if h.ConsecutivePasses != ConsecutivePassesRequired-1 {
		t.Errorf("consecutive passes = %d, want %d", h.ConsecutivePasses, ConsecutivePassesRequired-1)
	}

	// 7th run should trigger promotion.
	event := pt.RecordRun("test_scenario", UsuallyPasses, 1.0)
	if event == nil {
		t.Fatal("expected promotion event")
	}
	if event.Action != PromotionPromote {
		t.Errorf("action = %v, want promote", event.Action)
	}
	if event.ToPolicy != AlwaysPasses {
		t.Errorf("to_policy = %v, want AlwaysPasses", event.ToPolicy)
	}
}

func TestDemotionOnFailure(t *testing.T) {
	dir := t.TempDir()
	pt, err := NewPromotionTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	// First run as AlwaysPasses with failure should demote.
	event := pt.RecordRun("test_scenario", AlwaysPasses, 0.67)
	if event == nil {
		t.Fatal("expected demotion event")
	}
	if event.Action != PromotionDemote {
		t.Errorf("action = %v, want demote", event.Action)
	}
	if event.ToPolicy != UsuallyPasses {
		t.Errorf("to_policy = %v, want UsuallyPasses", event.ToPolicy)
	}
}

func TestFailureResetsConsecutiveCount(t *testing.T) {
	dir := t.TempDir()
	pt, err := NewPromotionTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 5 perfect runs.
	for i := 0; i < 5; i++ {
		pt.RecordRun("test_scenario", UsuallyPasses, 1.0)
	}

	// One failure resets the counter.
	pt.RecordRun("test_scenario", UsuallyPasses, 0.67)

	h := pt.Get("test_scenario")
	if h.ConsecutivePasses != 0 {
		t.Errorf("consecutive passes = %d, want 0 after failure", h.ConsecutivePasses)
	}

	// Need 7 more perfect runs now.
	for i := 0; i < ConsecutivePassesRequired-1; i++ {
		event := pt.RecordRun("test_scenario", UsuallyPasses, 1.0)
		if event != nil {
			t.Fatalf("unexpected promotion after %d runs post-failure", i+1)
		}
	}
	event := pt.RecordRun("test_scenario", UsuallyPasses, 1.0)
	if event == nil {
		t.Fatal("expected promotion event")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	// Create and populate.
	pt, err := NewPromotionTracker(dir)
	if err != nil {
		t.Fatal(err)
	}
	pt.RecordRun("scenario_a", UsuallyPasses, 1.0)
	pt.RecordRun("scenario_a", UsuallyPasses, 1.0)
	pt.RecordRun("scenario_b", AlwaysPasses, 0.5)
	if err := pt.Save(); err != nil {
		t.Fatal(err)
	}

	// Reload.
	pt2, err := NewPromotionTracker(dir)
	if err != nil {
		t.Fatal(err)
	}
	ha := pt2.Get("scenario_a")
	if ha == nil || ha.ConsecutivePasses != 2 {
		t.Errorf("scenario_a consecutive = %v, want 2", ha)
	}
	hb := pt2.Get("scenario_b")
	if hb == nil || hb.TotalRuns != 1 {
		t.Errorf("scenario_b total_runs = %v, want 1", hb)
	}
}

func TestReadyForPromotion(t *testing.T) {
	dir := t.TempDir()
	pt, err := NewPromotionTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 5 passes — within 2 of threshold.
	for i := 0; i < ConsecutivePassesRequired-2; i++ {
		pt.RecordRun("close_one", UsuallyPasses, 1.0)
	}
	// 1 pass — not close enough.
	pt.RecordRun("far_one", UsuallyPasses, 1.0)

	ready := pt.ReadyForPromotion()
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready scenario, got %d", len(ready))
	}
	if ready[0].Scenario != "close_one" {
		t.Errorf("expected close_one, got %s", ready[0].Scenario)
	}
}
