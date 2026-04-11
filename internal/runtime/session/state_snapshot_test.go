package session

import (
	"strings"
	"testing"
)

func TestUpdateStateSnapshot_NewFromSummary(t *testing.T) {
	summary := `<intent_summary>
Scope: 20 messages compacted
Primary Goal: implement authentication middleware
Working Set: internal/auth/middleware.go, internal/auth/handler.go
Verified Facts:
- Tests passing: ok github.com/example/auth
Active Blockers:
Tools Used: read_file, edit_file
</intent_summary>`

	snap := UpdateStateSnapshot(nil, summary)

	if snap.PrimaryGoal != "implement authentication middleware" {
		t.Errorf("unexpected goal: %q", snap.PrimaryGoal)
	}
	if len(snap.WorkingFiles) != 2 {
		t.Errorf("expected 2 working files, got %d", len(snap.WorkingFiles))
	}
	if snap.EnvironmentState != "tests passing" {
		t.Errorf("expected 'tests passing', got %q", snap.EnvironmentState)
	}
	if snap.CompactionCount != 0 {
		t.Errorf("expected 0 compactions for new snapshot, got %d", snap.CompactionCount)
	}
}

func TestUpdateStateSnapshot_Cumulative(t *testing.T) {
	// First compaction.
	summary1 := `Primary Goal: fix auth bug
Working Set: auth.go`
	snap := UpdateStateSnapshot(nil, summary1)

	// Second compaction.
	summary2 := `Primary Goal: add auth tests
Working Set: auth_test.go`
	snap = UpdateStateSnapshot(snap, summary2)

	if snap.CompactionCount != 1 {
		t.Errorf("expected 1 compaction, got %d", snap.CompactionCount)
	}
	if snap.PrimaryGoal != "add auth tests" {
		t.Errorf("expected updated goal, got %q", snap.PrimaryGoal)
	}
	if len(snap.CompletedSteps) != 1 {
		t.Errorf("expected 1 completed step, got %d", len(snap.CompletedSteps))
	}
	if snap.CompletedSteps[0] != "fix auth bug" {
		t.Errorf("expected completed step to be old goal, got %q", snap.CompletedSteps[0])
	}
}

func TestStateSnapshot_Format(t *testing.T) {
	snap := &StateSnapshot{
		PrimaryGoal:      "implement feature X",
		CompletedSteps:   []string{"design API"},
		CurrentStep:      "implement feature X",
		WorkingFiles:     []string{"api.go", "api_test.go"},
		EnvironmentState: "tests passing",
		CompactionCount:  2,
	}

	formatted := snap.Format()

	if !strings.Contains(formatted, "<state_snapshot>") {
		t.Error("expected state_snapshot tags")
	}
	if !strings.Contains(formatted, "Goal: implement feature X") {
		t.Error("expected goal in format")
	}
	if !strings.Contains(formatted, "Compactions: 2") {
		t.Error("expected compaction count in format")
	}
}

func TestStateSnapshot_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	snap := &StateSnapshot{
		PrimaryGoal:     "test save/load",
		CompactionCount: 3,
		WorkingFiles:    []string{"a.go"},
	}

	if err := SaveStateSnapshot(dir, snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadStateSnapshot(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if loaded.PrimaryGoal != "test save/load" {
		t.Errorf("unexpected goal: %q", loaded.PrimaryGoal)
	}
	if loaded.CompactionCount != 3 {
		t.Errorf("expected 3, got %d", loaded.CompactionCount)
	}
}

func TestStateSnapshot_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()

	snap, err := LoadStateSnapshot(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestUpdateStateSnapshot_BlockerState(t *testing.T) {
	summary := `Primary Goal: fix build
Active Blockers:
- compilation error in main.go`

	snap := UpdateStateSnapshot(nil, summary)
	if !strings.Contains(snap.EnvironmentState, "blocked") {
		t.Errorf("expected blocked state, got %q", snap.EnvironmentState)
	}
}

func TestUpdateStateSnapshot_CompletedStepsLimit(t *testing.T) {
	snap := &StateSnapshot{
		CurrentStep: "step 0",
	}
	// Fill up 12 completed steps.
	for i := 1; i <= 12; i++ {
		snap = UpdateStateSnapshot(snap, "Primary Goal: step "+string(rune('0'+i)))
	}

	// Should keep only last 10.
	if len(snap.CompletedSteps) > 10 {
		t.Errorf("expected at most 10 completed steps, got %d", len(snap.CompletedSteps))
	}
}
