package skillengine

import (
	"testing"
	"time"
)

func TestSkillMatchScore(t *testing.T) {
	skill := &SkillSpec{
		Name:            "tdd",
		TriggerPatterns: []string{`(?i)test.driven|tdd`},
		TriggerKeywords: []string{"test", "driven"},
		Stats:           SkillStats{Uses: 10, Successes: 8, SuccessRate: 0.8, DecayedScore: 0.8},
	}

	// Should match TDD pattern.
	score := skill.MatchScore("implement this using test-driven development")
	if score <= 0 {
		t.Fatalf("expected positive score, got %f", score)
	}

	// Should not match unrelated text.
	score = skill.MatchScore("deploy to production")
	if score > 0 {
		t.Fatalf("expected zero score, got %f", score)
	}
}

func TestRegistryFindBestMatch(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	tdd := &SkillSpec{
		Name:            "tdd",
		TriggerPatterns: []string{`(?i)test.driven|tdd`},
		Stats:           SkillStats{Uses: 5, SuccessRate: 0.9, DecayedScore: 0.9},
	}
	debug := &SkillSpec{
		Name:            "debug",
		TriggerPatterns: []string{`(?i)debug|troubleshoot`},
		Stats:           SkillStats{Uses: 3, SuccessRate: 0.7, DecayedScore: 0.7},
	}

	_ = reg.Register(tdd)
	_ = reg.Register(debug)

	// Should find TDD skill.
	match := reg.FindBestMatch("use TDD to implement this")
	if match == nil || match.Name != "tdd" {
		t.Fatalf("expected tdd, got %v", match)
	}

	// Should find debug skill.
	match = reg.FindBestMatch("debug this error")
	if match == nil || match.Name != "debug" {
		t.Fatalf("expected debug, got %v", match)
	}

	// No match.
	match = reg.FindBestMatch("deploy to staging")
	if match != nil {
		t.Fatalf("expected nil, got %v", match.Name)
	}
}

func TestRegistryRecordOutcome(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	_ = reg.Register(&SkillSpec{
		Name:            "test",
		TriggerPatterns: []string{`test`},
	})

	reg.RecordOutcome("test", true, 100)
	reg.RecordOutcome("test", true, 200)
	reg.RecordOutcome("test", false, 300)

	skill, ok := reg.Get("test")
	if !ok {
		t.Fatal("skill not found")
	}
	if skill.Stats.Uses != 3 {
		t.Fatalf("uses = %d, want 3", skill.Stats.Uses)
	}
	if skill.Stats.Successes != 2 {
		t.Fatalf("successes = %d, want 2", skill.Stats.Successes)
	}
}

func TestApplyDecay(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	_ = reg.Register(&SkillSpec{
		Name: "old_skill",
		Stats: SkillStats{
			Uses:         10,
			Successes:    8,
			SuccessRate:  0.8,
			DecayedScore: 0.8,
			LastUsed:     time.Now().Add(-14 * 24 * time.Hour), // 2 weeks ago
		},
	})

	reg.ApplyDecay()

	skill, _ := reg.Get("old_skill")
	if skill.Stats.DecayedScore >= 0.8 {
		t.Fatalf("expected decayed score < 0.8, got %f", skill.Stats.DecayedScore)
	}
	if skill.Stats.DecayedScore <= 0 {
		t.Fatalf("decayed score should be positive, got %f", skill.Stats.DecayedScore)
	}
}

func TestEvolutionFix(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	_ = reg.Register(&SkillSpec{
		Name:        "broken",
		Version:     1,
		Instruction: "old instruction",
	})

	ev := NewEvolver(reg)
	fixed, err := ev.FixSkill("broken", "fixed instruction", "test failure")
	if err != nil {
		t.Fatalf("FixSkill: %v", err)
	}

	if fixed.Version != 2 {
		t.Fatalf("version = %d, want 2", fixed.Version)
	}
	if fixed.EvolutionMode != EvolutionFix {
		t.Fatalf("mode = %s, want fix", fixed.EvolutionMode)
	}
	if fixed.Instruction != "fixed instruction" {
		t.Fatal("instruction not updated")
	}
}

func TestEvolutionDerived(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	_ = reg.Register(&SkillSpec{
		Name:            "parent",
		Description:     "parent skill",
		TriggerKeywords: []string{"test"},
	})

	ev := NewEvolver(reg)
	derived, err := ev.DeriveSkill("parent", "specialized instruction", "golang")
	if err != nil {
		t.Fatalf("DeriveSkill: %v", err)
	}

	if derived.EvolutionMode != EvolutionDerived {
		t.Fatalf("mode = %s, want derived", derived.EvolutionMode)
	}
	if derived.Parent != "parent" {
		t.Fatalf("parent = %s, want parent", derived.Parent)
	}
}

func TestEvolutionCaptured(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	ev := NewEvolver(reg)
	captured, err := ev.CaptureSkill("new_pattern", "discovered pattern", "do X then Y", []string{"pattern"})
	if err != nil {
		t.Fatalf("CaptureSkill: %v", err)
	}

	if captured.EvolutionMode != EvolutionCaptured {
		t.Fatalf("mode = %s, want captured", captured.EvolutionMode)
	}

	// Verify it's in the registry.
	_, ok := reg.Get("new_pattern")
	if !ok {
		t.Fatal("captured skill not in registry")
	}
}

func TestEngineSelectSkill(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	_ = reg.Register(&SkillSpec{
		Name:            "tdd",
		TriggerPatterns: []string{`(?i)test.driven|tdd`},
		Stats:           SkillStats{Uses: 5, SuccessRate: 0.9, DecayedScore: 0.9},
	})

	engine := NewEngine(reg)
	skill := engine.SelectSkill("let's use TDD")
	if skill == nil {
		t.Fatal("expected skill match")
	}
	if skill.Name != "tdd" {
		t.Fatalf("expected tdd, got %s", skill.Name)
	}
}
