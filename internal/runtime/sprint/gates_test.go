package sprint

import (
	"fmt"
	"testing"
)

func makeTestState(statuses ...TaskStatus) *SprintState {
	tasks := make([]SprintTask, len(statuses))
	for i, s := range statuses {
		tasks[i] = SprintTask{ID: fmt.Sprintf("t%d", i+1), Status: s}
	}
	return &SprintState{
		Milestone: &SprintMilestone{
			ID: "m1",
			Slices: []SprintSlice{
				{ID: "s1", Tasks: tasks},
			},
		},
	}
}

func TestAllTasksCompleteGate_AllDone(t *testing.T) {
	state := makeTestState(TaskCompleted, TaskCompleted, TaskFailed)
	gate := AllTasksCompleteGate{}

	ok, reason := gate.CanAdvance(state)
	if !ok {
		t.Errorf("expected advance allowed, got blocked: %s", reason)
	}
}

func TestAllTasksCompleteGate_PendingBlocks(t *testing.T) {
	state := makeTestState(TaskCompleted, TaskPending)
	gate := AllTasksCompleteGate{}

	ok, reason := gate.CanAdvance(state)
	if ok {
		t.Error("expected gate to block with pending task")
	}
	if reason == "" {
		t.Error("expected blocking reason")
	}
}

func TestAllTasksCompleteGate_RunningBlocks(t *testing.T) {
	state := makeTestState(TaskRunning)
	gate := AllTasksCompleteGate{}

	ok, _ := gate.CanAdvance(state)
	if ok {
		t.Error("expected gate to block with running task")
	}
}

func TestScoreThresholdGate_AboveThreshold(t *testing.T) {
	state := makeTestState(TaskCompleted)
	state.Milestone.EvalAfter = 0.8
	gate := ScoreThresholdGate{MinScore: 0.7}

	ok, _ := gate.CanAdvance(state)
	if !ok {
		t.Error("expected advance allowed when score above threshold")
	}
}

func TestScoreThresholdGate_BelowThreshold(t *testing.T) {
	state := makeTestState(TaskCompleted)
	state.Milestone.EvalAfter = 0.5
	gate := ScoreThresholdGate{MinScore: 0.7}

	ok, reason := gate.CanAdvance(state)
	if ok {
		t.Error("expected gate to block when score below threshold")
	}
	if reason == "" {
		t.Error("expected blocking reason")
	}
}

func TestNoFailedTasksGate_NoFailures(t *testing.T) {
	state := makeTestState(TaskCompleted, TaskCompleted, TaskPending)
	gate := NoFailedTasksGate{}

	ok, _ := gate.CanAdvance(state)
	if !ok {
		t.Error("expected advance allowed with no failures")
	}
}

func TestNoFailedTasksGate_FailureBlocks(t *testing.T) {
	state := makeTestState(TaskCompleted, TaskFailed)
	gate := NoFailedTasksGate{}

	ok, reason := gate.CanAdvance(state)
	if ok {
		t.Error("expected gate to block with failed task")
	}
	if reason == "" {
		t.Error("expected blocking reason")
	}
}

func TestBudgetGate_WithinBudget(t *testing.T) {
	state := &SprintState{Budget: 1000, TokensUsed: 500}
	gate := BudgetGate{}

	ok, _ := gate.CanAdvance(state)
	if !ok {
		t.Error("expected advance allowed within budget")
	}
}

func TestBudgetGate_Exceeded(t *testing.T) {
	state := &SprintState{Budget: 1000, TokensUsed: 1500}
	gate := BudgetGate{}

	ok, reason := gate.CanAdvance(state)
	if ok {
		t.Error("expected gate to block when budget exceeded")
	}
	if reason == "" {
		t.Error("expected blocking reason")
	}
}

func TestBudgetGate_UnlimitedBudget(t *testing.T) {
	state := &SprintState{Budget: 0, TokensUsed: 999999}
	gate := BudgetGate{}

	ok, _ := gate.CanAdvance(state)
	if !ok {
		t.Error("expected advance allowed with unlimited budget (0)")
	}
}

func TestCompositeGate_AllPass(t *testing.T) {
	state := makeTestState(TaskCompleted)
	state.Milestone.EvalAfter = 0.9

	gate := CompositeGate{
		Gates: []PhaseGate{
			AllTasksCompleteGate{},
			ScoreThresholdGate{MinScore: 0.5},
		},
	}

	ok, _ := gate.CanAdvance(state)
	if !ok {
		t.Error("expected composite gate to pass when all gates pass")
	}
}

func TestCompositeGate_OneBlocks(t *testing.T) {
	state := makeTestState(TaskCompleted)
	state.Milestone.EvalAfter = 0.3

	gate := CompositeGate{
		Gates: []PhaseGate{
			AllTasksCompleteGate{},
			ScoreThresholdGate{MinScore: 0.5},
		},
	}

	ok, _ := gate.CanAdvance(state)
	if ok {
		t.Error("expected composite gate to block when one gate fails")
	}
}

func TestCheckGates_MultipleGates(t *testing.T) {
	state := makeTestState(TaskCompleted, TaskCompleted)
	state.Milestone.EvalAfter = 0.8

	ok, _ := CheckGates(state,
		AllTasksCompleteGate{},
		ScoreThresholdGate{MinScore: 0.7},
		NoFailedTasksGate{},
	)
	if !ok {
		t.Error("expected all gates to pass")
	}
}

func TestGates_NilState(t *testing.T) {
	gates := []PhaseGate{
		AllTasksCompleteGate{},
		ScoreThresholdGate{MinScore: 0.5},
		NoFailedTasksGate{},
		BudgetGate{},
	}

	for _, gate := range gates {
		ok, reason := gate.CanAdvance(nil)
		if ok {
			t.Error("expected gate to block on nil state")
		}
		if reason == "" {
			t.Error("expected reason for nil state")
		}
	}
}
