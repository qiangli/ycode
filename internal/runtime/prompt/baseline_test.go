package prompt

import (
	"sort"
	"testing"
)

func TestContextBaseline_FirstTurnIsAlwaysFull(t *testing.T) {
	bl := NewContextBaseline()
	current := map[string]string{
		"environment": "some env",
		"git":         "some git",
	}

	diff := bl.Diff(current)
	if !diff.IsFirst {
		t.Error("expected IsFirst=true on first diff")
	}
	if len(diff.Unchanged) != 0 {
		t.Errorf("expected no unchanged sections on first diff, got %v", diff.Unchanged)
	}
	if len(diff.Changed) != 2 {
		t.Errorf("expected 2 changed sections, got %d", len(diff.Changed))
	}
}

func TestContextBaseline_UnchangedSectionsDetected(t *testing.T) {
	bl := NewContextBaseline()
	sections := map[string]string{
		"environment":  "env content",
		"git":          "git content",
		"instructions": "instr content",
	}

	// First turn: update baseline.
	bl.Update(sections)

	// Second turn: same content.
	diff := bl.Diff(sections)
	if diff.IsFirst {
		t.Error("expected IsFirst=false after update")
	}

	sort.Strings(diff.Unchanged)
	if len(diff.Unchanged) != 3 {
		t.Errorf("expected 3 unchanged, got %d: %v", len(diff.Unchanged), diff.Unchanged)
	}
	if len(diff.Changed) != 0 {
		t.Errorf("expected 0 changed, got %d: %v", len(diff.Changed), diff.Changed)
	}
}

func TestContextBaseline_ChangedSectionDetected(t *testing.T) {
	bl := NewContextBaseline()
	v1 := map[string]string{
		"environment": "env v1",
		"git":         "git v1",
	}
	bl.Update(v1)

	v2 := map[string]string{
		"environment": "env v1",   // unchanged
		"git":         "git v2!!", // changed
	}
	diff := bl.Diff(v2)

	if len(diff.Changed) != 1 || diff.Changed[0] != "git" {
		t.Errorf("expected [git] changed, got %v", diff.Changed)
	}
	if len(diff.Unchanged) != 1 || diff.Unchanged[0] != "environment" {
		t.Errorf("expected [environment] unchanged, got %v", diff.Unchanged)
	}
}

func TestContextBaseline_ResetForcesFullSend(t *testing.T) {
	bl := NewContextBaseline()
	sections := map[string]string{"a": "content"}
	bl.Update(sections)

	if bl.TurnNumber() != 1 {
		t.Errorf("expected turn 1, got %d", bl.TurnNumber())
	}

	bl.Reset()

	if bl.TurnNumber() != 0 {
		t.Errorf("expected turn 0 after reset, got %d", bl.TurnNumber())
	}

	diff := bl.Diff(sections)
	if !diff.IsFirst {
		t.Error("expected IsFirst=true after reset")
	}
}

func TestContextBaseline_NewSectionAlwaysChanged(t *testing.T) {
	bl := NewContextBaseline()
	bl.Update(map[string]string{"a": "content"})

	diff := bl.Diff(map[string]string{
		"a": "content",
		"b": "new section",
	})

	if len(diff.Changed) != 1 || diff.Changed[0] != "b" {
		t.Errorf("expected [b] as changed (new section), got %v", diff.Changed)
	}
}
