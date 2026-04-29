package sprint

import (
	"testing"
)

func TestSprintStateLifecycle(t *testing.T) {
	state := NewSprintState("add health check", 10000)

	if state.Phase != PhasePlan {
		t.Fatalf("initial phase = %s, want plan", state.Phase)
	}

	// Set up a milestone with tasks.
	state.Milestone = &SprintMilestone{
		ID:   "M01",
		Goal: "add health check",
		Slices: []SprintSlice{
			{
				ID:          "S01",
				MilestoneID: "M01",
				Tasks: []SprintTask{
					{ID: "T01", Description: "create endpoint", Status: TaskPending},
					{ID: "T02", Description: "add tests", Status: TaskPending},
				},
			},
		},
	}

	// First task.
	task := state.CurrentTaskRef()
	if task == nil {
		t.Fatal("expected current task")
	}
	if task.ID != "T01" {
		t.Fatalf("expected T01, got %s", task.ID)
	}

	// Advance to second task.
	if !state.AdvanceTask() {
		t.Fatal("expected more tasks")
	}
	task = state.CurrentTaskRef()
	if task.ID != "T02" {
		t.Fatalf("expected T02, got %s", task.ID)
	}

	// Advance past all tasks.
	if state.AdvanceTask() {
		t.Fatal("expected no more tasks")
	}
}

func TestSprintStateSaveLoad(t *testing.T) {
	dir := t.TempDir()

	state := NewSprintState("test goal", 5000)
	state.Milestone = &SprintMilestone{
		ID:   "M01",
		Goal: "test goal",
		Slices: []SprintSlice{
			{
				ID:    "S01",
				Tasks: []SprintTask{{ID: "T01", Description: "task 1", Status: TaskPending}},
			},
		},
	}

	if err := state.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadSprintState(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Goal != "test goal" {
		t.Fatalf("goal = %s, want 'test goal'", loaded.Goal)
	}
	if loaded.Budget != 5000 {
		t.Fatalf("budget = %d, want 5000", loaded.Budget)
	}
	if len(loaded.Milestone.Slices) != 1 {
		t.Fatalf("slices = %d, want 1", len(loaded.Milestone.Slices))
	}
}

func TestAllTasksComplete(t *testing.T) {
	state := NewSprintState("test", 0)
	state.Milestone = &SprintMilestone{
		Slices: []SprintSlice{
			{
				Tasks: []SprintTask{
					{ID: "T01", Status: TaskCompleted},
					{ID: "T02", Status: TaskCompleted},
				},
			},
		},
	}

	if !state.AllTasksComplete() {
		t.Fatal("expected all tasks complete")
	}

	// Mark one as pending.
	state.Milestone.Slices[0].Tasks[1].Status = TaskPending
	if state.AllTasksComplete() {
		t.Fatal("expected not all tasks complete")
	}
}

func TestBudgetExceeded(t *testing.T) {
	state := NewSprintState("test", 1000)
	if state.BudgetExceeded() {
		t.Fatal("budget should not be exceeded initially")
	}

	state.TokensUsed = 1000
	if !state.BudgetExceeded() {
		t.Fatal("budget should be exceeded")
	}

	// Zero budget = unlimited.
	state.Budget = 0
	if state.BudgetExceeded() {
		t.Fatal("zero budget should mean unlimited")
	}
}

func TestDecomposeGoal(t *testing.T) {
	result := DecomposeGoal("add feature", []string{"task 1", "task 2", "task 3"})

	if result.Milestone == nil {
		t.Fatal("expected milestone")
	}
	if result.Milestone.Goal != "add feature" {
		t.Fatalf("goal = %s", result.Milestone.Goal)
	}
	if len(result.Milestone.Slices) != 1 {
		t.Fatalf("slices = %d, want 1", len(result.Milestone.Slices))
	}
	if len(result.Milestone.Slices[0].Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(result.Milestone.Slices[0].Tasks))
	}
}
