package sprint

import "fmt"

// PhaseGate defines a condition that must be met before a sprint phase can advance.
// Inspired by ClawTeam's ArtifactRequiredGate, AllTasksCompleteGate, and
// HumanApprovalGate patterns. Gates are declarative and composable.
type PhaseGate interface {
	// CanAdvance checks if the gate allows phase transition.
	// Returns true if the gate is satisfied. If not, reason explains why.
	CanAdvance(state *SprintState) (ok bool, reason string)
}

// AllTasksCompleteGate blocks phase advancement until all tasks in the current
// milestone have a terminal status (completed or failed).
type AllTasksCompleteGate struct{}

func (g AllTasksCompleteGate) CanAdvance(state *SprintState) (bool, string) {
	if state == nil || state.Milestone == nil {
		return false, "no sprint state or milestone"
	}
	for _, s := range state.Milestone.Slices {
		for _, task := range s.Tasks {
			if task.Status != TaskCompleted && task.Status != TaskFailed {
				return false, fmt.Sprintf("task %s is %s (not complete)", task.ID, task.Status)
			}
		}
	}
	return true, ""
}

// ScoreThresholdGate blocks phase advancement until the milestone's eval-after
// score meets or exceeds the threshold.
type ScoreThresholdGate struct {
	MinScore float64
}

func (g ScoreThresholdGate) CanAdvance(state *SprintState) (bool, string) {
	if state == nil || state.Milestone == nil {
		return false, "no sprint state or milestone"
	}
	if state.Milestone.EvalAfter < g.MinScore {
		return false, fmt.Sprintf("eval score %.2f below threshold %.2f", state.Milestone.EvalAfter, g.MinScore)
	}
	return true, ""
}

// NoFailedTasksGate blocks phase advancement if any tasks have failed.
// Stricter than AllTasksCompleteGate.
type NoFailedTasksGate struct{}

func (g NoFailedTasksGate) CanAdvance(state *SprintState) (bool, string) {
	if state == nil || state.Milestone == nil {
		return false, "no sprint state or milestone"
	}
	for _, s := range state.Milestone.Slices {
		for _, task := range s.Tasks {
			if task.Status == TaskFailed {
				return false, fmt.Sprintf("task %s has failed", task.ID)
			}
		}
	}
	return true, ""
}

// BudgetGate blocks phase advancement if the token budget has been exceeded.
type BudgetGate struct{}

func (g BudgetGate) CanAdvance(state *SprintState) (bool, string) {
	if state == nil {
		return false, "no sprint state"
	}
	if state.BudgetExceeded() {
		return false, fmt.Sprintf("budget exceeded: %d/%d tokens used", state.TokensUsed, state.Budget)
	}
	return true, ""
}

// CompositeGate combines multiple gates with AND semantics.
// All gates must pass for the composite to pass.
type CompositeGate struct {
	Gates []PhaseGate
}

func (g CompositeGate) CanAdvance(state *SprintState) (bool, string) {
	for _, gate := range g.Gates {
		if ok, reason := gate.CanAdvance(state); !ok {
			return false, reason
		}
	}
	return true, ""
}

// CheckGates evaluates all gates and returns whether advancement is allowed.
// If any gate blocks, returns the first blocking reason.
func CheckGates(state *SprintState, gates ...PhaseGate) (bool, string) {
	for _, gate := range gates {
		if ok, reason := gate.CanAdvance(state); !ok {
			return false, reason
		}
	}
	return true, ""
}
