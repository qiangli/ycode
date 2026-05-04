package sprint

import (
	"time"
)

// RunStatus represents the lifecycle of a single task execution run.
// Inspired by paperclip's heartbeat runs (queued→running→succeeded/failed/timed_out)
// and agent-orchestrator's canonical lifecycle with per-run tracking.
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunTimedOut  RunStatus = "timed_out"
	RunCancelled RunStatus = "cancelled"
)

// TaskRun records a single execution attempt for a task.
// Multiple runs may exist per task (retries, continuation runs).
type TaskRun struct {
	// RunID uniquely identifies this execution attempt.
	RunID string `json:"run_id"`

	// TaskID references the parent task.
	TaskID string `json:"task_id"`

	// AgentType is the agent that executed this run (e.g., "claude", "codex", "ycode").
	AgentType string `json:"agent_type,omitempty"`

	// Status tracks the run lifecycle.
	Status RunStatus `json:"status"`

	// Attempt is the retry number (1-indexed).
	Attempt int `json:"attempt"`

	// StartedAt records when execution began.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt records when execution finished (success, failure, or timeout).
	CompletedAt time.Time `json:"completed_at,omitempty"`

	// Duration of the run.
	Duration time.Duration `json:"duration,omitempty"`

	// Output is the primary result text from the agent.
	Output string `json:"output,omitempty"`

	// Error is the error message if the run failed.
	Error string `json:"error,omitempty"`

	// ExitCode from the agent process (-1 for timeout).
	ExitCode int `json:"exit_code"`

	// SessionID for session continuity across retries.
	SessionID string `json:"session_id,omitempty"`

	// TokensUsed is the total token count for this run.
	TokensUsed int `json:"tokens_used"`

	// FilesModified is the count of files changed.
	FilesModified int `json:"files_modified"`
}

// RunTracker accumulates execution runs for tasks in a sprint.
// It provides per-task run history and aggregate metrics.
type RunTracker struct {
	runs map[string][]*TaskRun // taskID → runs (ordered by attempt)
}

// NewRunTracker creates a tracker.
func NewRunTracker() *RunTracker {
	return &RunTracker{
		runs: make(map[string][]*TaskRun),
	}
}

// RecordRun adds a completed run to the tracker.
func (rt *RunTracker) RecordRun(run *TaskRun) {
	rt.runs[run.TaskID] = append(rt.runs[run.TaskID], run)
}

// RunsForTask returns all runs for a task, ordered by attempt.
func (rt *RunTracker) RunsForTask(taskID string) []*TaskRun {
	return rt.runs[taskID]
}

// LastRun returns the most recent run for a task.
func (rt *RunTracker) LastRun(taskID string) *TaskRun {
	runs := rt.runs[taskID]
	if len(runs) == 0 {
		return nil
	}
	return runs[len(runs)-1]
}

// TotalTokens returns the total tokens used across all runs.
func (rt *RunTracker) TotalTokens() int {
	total := 0
	for _, runs := range rt.runs {
		for _, r := range runs {
			total += r.TokensUsed
		}
	}
	return total
}

// TotalDuration returns the total wall-clock time across all runs.
func (rt *RunTracker) TotalDuration() time.Duration {
	var total time.Duration
	for _, runs := range rt.runs {
		for _, r := range runs {
			total += r.Duration
		}
	}
	return total
}

// SuccessRate returns the fraction of runs that succeeded.
func (rt *RunTracker) SuccessRate() float64 {
	total := 0
	succeeded := 0
	for _, runs := range rt.runs {
		for _, r := range runs {
			total++
			if r.Status == RunSucceeded {
				succeeded++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(succeeded) / float64(total)
}

// Summary returns a human-readable summary of all tracked runs.
func (rt *RunTracker) Summary() RunSummary {
	s := RunSummary{}
	for _, runs := range rt.runs {
		for _, r := range runs {
			s.TotalRuns++
			s.TotalTokens += r.TokensUsed
			s.TotalDuration += r.Duration
			switch r.Status {
			case RunSucceeded:
				s.Succeeded++
			case RunFailed:
				s.Failed++
			case RunTimedOut:
				s.TimedOut++
			case RunCancelled:
				s.Cancelled++
			}
		}
	}
	return s
}

// RunSummary aggregates metrics across all runs.
type RunSummary struct {
	TotalRuns     int
	Succeeded     int
	Failed        int
	TimedOut      int
	Cancelled     int
	TotalTokens   int
	TotalDuration time.Duration
}
