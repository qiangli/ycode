package cascade

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func newTestEscalator(t *testing.T, cfg Config) *Escalator {
	t.Helper()
	e := New(cfg)
	if e == nil {
		t.Fatalf("New(%v) = nil, want an escalator", cfg.Ladder)
	}
	return e
}

// TestLoopEscalatesImmediately: a detected loop is the strongest stuck signal —
// one Observe(SignalLoop) must move the served model up a rung.
func TestLoopEscalatesImmediately(t *testing.T) {
	var got []Switch
	e := newTestEscalator(t, Config{
		Ladder:   []string{"base", "premium", "top"},
		OnSwitch: func(sw Switch) { got = append(got, sw) },
	})

	if e.Escalated() {
		t.Fatal("fresh escalator reports escalated")
	}
	if e.Model() != "base" || e.Base() != "base" {
		t.Fatalf("start: Model=%q Base=%q, want base/base", e.Model(), e.Base())
	}

	sw, err := e.Observe(SignalLoop)
	if err != nil {
		t.Fatalf("Observe(loop): %v", err)
	}
	if sw == nil {
		t.Fatal("Observe(loop) returned no switch; a loop must escalate immediately")
	}
	if sw.From != "base" || sw.To != "premium" || sw.Rung != 1 || sw.Reason != "loop" {
		t.Errorf("switch = %+v, want base→premium rung 1 reason loop", sw)
	}
	if e.Model() != "premium" || !e.Escalated() || e.Reason() != "loop" {
		t.Errorf("after switch: Model=%q Escalated=%v Reason=%q", e.Model(), e.Escalated(), e.Reason())
	}
	if len(got) != 1 || got[0] != *sw {
		t.Errorf("OnSwitch calls = %v, want exactly the returned switch", got)
	}
}

// TestStallThresholdAndProgressReset: stalls escalate only when consecutive;
// any progress in between resets the count.
func TestStallThresholdAndProgressReset(t *testing.T) {
	e := newTestEscalator(t, Config{Ladder: []string{"base", "premium"}, StallThreshold: 3})

	// Two stalls, then progress: no escalation.
	for _, sig := range []Signal{SignalStall, SignalStall, SignalProgress, SignalStall, SignalStall} {
		sw, err := e.Observe(sig)
		if err != nil || sw != nil {
			t.Fatalf("Observe(%v) = %v, %v; want no switch yet", sig, sw, err)
		}
	}
	// Third consecutive stall escalates, with a reason that says how many.
	sw, err := e.Observe(SignalStall)
	if err != nil {
		t.Fatalf("Observe(third stall): %v", err)
	}
	if sw == nil || sw.To != "premium" || sw.Reason != "stall_x3" {
		t.Fatalf("switch = %+v, want →premium reason stall_x3", sw)
	}
}

// TestProgressNeverDemotes: once escalated, progress keeps the premium rung —
// dropping back to the model that got stuck just re-enters the hole.
func TestProgressNeverDemotes(t *testing.T) {
	e := newTestEscalator(t, Config{Ladder: []string{"base", "premium"}})
	if _, err := e.Observe(SignalLoop); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if sw, err := e.Observe(SignalProgress); sw != nil || err != nil {
			t.Fatalf("progress after escalation: %v, %v", sw, err)
		}
	}
	if e.Model() != "premium" {
		t.Errorf("Model = %q after progress, want premium (no demotion)", e.Model())
	}
}

// TestProbeSkipsUnavailableRung: a rung with no credentials is skipped, and the
// climb lands on the next reachable one.
func TestProbeSkipsUnavailableRung(t *testing.T) {
	e := newTestEscalator(t, Config{
		Ladder: []string{"base", "premium", "top"},
		Probe: func(model string) error {
			if model == "premium" {
				return errors.New("no API key")
			}
			return nil
		},
	})
	sw, err := e.Observe(SignalLoop)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if sw == nil || sw.To != "top" || sw.Rung != 2 {
		t.Fatalf("switch = %+v, want →top rung 2 (premium skipped)", sw)
	}
}

// TestAllTiersUnavailable: escalation warranted but every rung is unreachable —
// the error must be LOUD and name each dead tier; the run stays on base.
func TestAllTiersUnavailable(t *testing.T) {
	e := newTestEscalator(t, Config{
		Ladder: []string{"base", "premium", "top"},
		Probe:  func(model string) error { return fmt.Errorf("quota exhausted for %s", model) },
	})
	sw, err := e.Observe(SignalLoop)
	if sw != nil {
		t.Fatalf("switch = %+v, want none when every tier is unavailable", sw)
	}
	if err == nil {
		t.Fatal("no error for all-tiers-unavailable; the failure must surface")
	}
	for _, want := range []string{"base", "premium", "top", "unavailable", "loop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if e.Model() != "base" || e.Escalated() {
		t.Errorf("failed escalation mutated state: Model=%q Escalated=%v", e.Model(), e.Escalated())
	}
}

// TestLadderExhausted: on the top rung, a further loop reports ErrLadderExhausted.
func TestLadderExhausted(t *testing.T) {
	e := newTestEscalator(t, Config{Ladder: []string{"base", "top"}})
	if _, err := e.Observe(SignalLoop); err != nil {
		t.Fatal(err)
	}
	sw, err := e.Observe(SignalLoop)
	if sw != nil {
		t.Fatalf("switch above the top rung: %+v", sw)
	}
	if !errors.Is(err, ErrLadderExhausted) {
		t.Errorf("error = %v, want ErrLadderExhausted", err)
	}
}

// TestForce escalates regardless of accumulated signals.
func TestForce(t *testing.T) {
	e := newTestEscalator(t, Config{Ladder: []string{"base", "premium"}})
	sw, err := e.Force("")
	if err != nil || sw == nil || sw.To != "premium" || sw.Reason != "forced" {
		t.Fatalf("Force = %+v, %v; want →premium reason forced", sw, err)
	}
}

// TestDegenerateLadders: fewer than two usable rungs means no escalator at all,
// and every method on the resulting nil receiver is a safe no-op.
func TestDegenerateLadders(t *testing.T) {
	for _, ladder := range [][]string{nil, {}, {"only"}, {" ", ""}} {
		if e := New(Config{Ladder: ladder}); e != nil {
			t.Errorf("New(%q) = %+v, want nil", ladder, e)
		}
	}

	var e *Escalator
	if e.Model() != "" || e.Base() != "" || e.Escalated() || e.Reason() != "" || e.Ladder() != nil {
		t.Error("nil escalator getters not zero-valued")
	}
	if sw, err := e.Observe(SignalLoop); sw != nil || err != nil {
		t.Errorf("nil Observe = %v, %v; want no-op", sw, err)
	}
	if sw, err := e.Force("x"); sw != nil || err != nil {
		t.Errorf("nil Force = %v, %v; want no-op", sw, err)
	}
}
