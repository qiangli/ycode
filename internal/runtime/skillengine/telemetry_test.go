package skillengine

import (
	"regexp"
	"testing"
	"time"
)

func TestSpec_MatchesTelemetry(t *testing.T) {
	s := &SkillSpec{
		Name:              "selfheal-abc",
		TelemetryTriggers: []string{regexp.QuoteMeta("action evaluate not supported")},
	}
	if !s.MatchesTelemetry("action evaluate not supported") {
		t.Fatal("expected exact match")
	}
	if !s.MatchesTelemetry("prefix action evaluate not supported suffix") {
		t.Fatal("expected substring match (regex contains)")
	}
	if s.MatchesTelemetry("completely different error") {
		t.Fatal("non-matching string should not match")
	}
	if s.MatchesTelemetry("") {
		t.Fatal("empty error should not match")
	}
}

func TestRegistry_RecallByTelemetry_SortByDecayedScore(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)
	good := &SkillSpec{
		Name:              "good-fix",
		TelemetryTriggers: []string{`panic`},
		Stats:             SkillStats{Uses: 10, Successes: 9, SuccessRate: 0.9, DecayedScore: 0.9, LastUsed: time.Now()},
	}
	better := &SkillSpec{
		Name:              "better-fix",
		TelemetryTriggers: []string{`panic`},
		Stats:             SkillStats{Uses: 5, Successes: 5, SuccessRate: 1.0, DecayedScore: 1.0, LastUsed: time.Now()},
	}
	unrelated := &SkillSpec{
		Name:              "other",
		TelemetryTriggers: []string{`not-a-match`},
	}
	for _, s := range []*SkillSpec{good, better, unrelated} {
		if err := r.Register(s); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	hits := r.RecallByTelemetry("panic: runtime error")
	if len(hits) != 2 {
		t.Fatalf("hit count = %d; want 2 (panic-keyed; unrelated should not match)", len(hits))
	}
	if hits[0].Name != "better-fix" {
		t.Fatalf("first hit = %q; want better-fix (higher decayed score)", hits[0].Name)
	}
}

func TestRegistry_RecallByTelemetry_EmptyAndNoMatch(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if r.RecallByTelemetry("") != nil {
		t.Fatal("empty normalized error must return nil")
	}
	if r.RecallByTelemetry("anything") != nil {
		t.Fatal("empty registry must return nil")
	}
}
