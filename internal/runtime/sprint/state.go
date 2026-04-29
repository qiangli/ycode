// Package sprint implements a structured autonomous development workflow:
// Plan → Execute → Complete → Reassess → ValidateMilestone.
//
// The hierarchy is Milestone → Slice → Task, where each leaf task
// fits in one context window and gets a fresh conversation runtime.
package sprint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Phase represents the sprint state machine phase.
type Phase string

const (
	PhasePlan              Phase = "plan"
	PhaseExecute           Phase = "execute"
	PhaseComplete          Phase = "complete"
	PhaseReassess          Phase = "reassess"
	PhaseValidateMilestone Phase = "validate_milestone"
	PhaseDone              Phase = "done"
	PhaseFailed            Phase = "failed"
)

// TaskStatus tracks leaf task completion.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskReview    TaskStatus = "review"
)

// SprintTask is a leaf-level task that fits in one context window.
type SprintTask struct {
	ID                 string     `json:"id"`
	SliceID            string     `json:"slice_id"`
	Description        string     `json:"description"`
	AcceptanceCriteria []string   `json:"acceptance_criteria,omitempty"`
	Status             TaskStatus `json:"status"`
	Output             string     `json:"output,omitempty"`
	ReviewFeedback     string     `json:"review_feedback,omitempty"`
	Attempts           int        `json:"attempts"`
	TokensUsed         int        `json:"tokens_used"`
}

// SprintSlice is a group of tasks that together deliver one demoable capability.
type SprintSlice struct {
	ID          string       `json:"id"`
	MilestoneID string       `json:"milestone_id"`
	Description string       `json:"description"`
	Tasks       []SprintTask `json:"tasks"`
}

// SprintMilestone is a major deliverable composed of slices.
type SprintMilestone struct {
	ID         string        `json:"id"`
	Goal       string        `json:"goal"`
	Slices     []SprintSlice `json:"slices"`
	EvalBefore float64       `json:"eval_before,omitempty"` // pre-implementation eval score
	EvalAfter  float64       `json:"eval_after,omitempty"`  // post-implementation eval score
}

// SprintState holds the full sprint execution state, persisted to disk.
type SprintState struct {
	Phase        Phase            `json:"phase"`
	Goal         string           `json:"goal"`
	Milestone    *SprintMilestone `json:"milestone,omitempty"`
	CurrentSlice int              `json:"current_slice"`
	CurrentTask  int              `json:"current_task"`
	Budget       int              `json:"budget"`      // max tokens
	TokensUsed   int              `json:"tokens_used"` // total tokens consumed
	StartedAt    time.Time        `json:"started_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	Learnings    []string         `json:"learnings,omitempty"`
	path         string
}

// NewSprintState creates a new sprint state for the given goal.
func NewSprintState(goal string, budget int) *SprintState {
	return &SprintState{
		Phase:     PhasePlan,
		Goal:      goal,
		Budget:    budget,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// Save persists the sprint state to disk.
func (s *SprintState) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sprint dir: %w", err)
	}
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	s.path = filepath.Join(dir, "sprint_state.json")
	return os.WriteFile(s.path, data, 0o644)
}

// Load reads sprint state from disk.
func LoadSprintState(dir string) (*SprintState, error) {
	path := filepath.Join(dir, "sprint_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s SprintState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.path = path
	return &s, nil
}

// CurrentTaskRef returns the current task being worked on.
func (s *SprintState) CurrentTaskRef() *SprintTask {
	if s.Milestone == nil {
		return nil
	}
	if s.CurrentSlice >= len(s.Milestone.Slices) {
		return nil
	}
	slice := &s.Milestone.Slices[s.CurrentSlice]
	if s.CurrentTask >= len(slice.Tasks) {
		return nil
	}
	return &slice.Tasks[s.CurrentTask]
}

// AdvanceTask moves to the next task. Returns false if all tasks are done.
func (s *SprintState) AdvanceTask() bool {
	if s.Milestone == nil {
		return false
	}
	slice := &s.Milestone.Slices[s.CurrentSlice]
	s.CurrentTask++
	if s.CurrentTask >= len(slice.Tasks) {
		s.CurrentTask = 0
		s.CurrentSlice++
		if s.CurrentSlice >= len(s.Milestone.Slices) {
			return false
		}
	}
	return true
}

// BudgetExceeded returns true if token budget is consumed.
func (s *SprintState) BudgetExceeded() bool {
	return s.Budget > 0 && s.TokensUsed >= s.Budget
}

// AllTasksComplete returns true if all tasks in the milestone are completed.
func (s *SprintState) AllTasksComplete() bool {
	if s.Milestone == nil {
		return false
	}
	for _, slice := range s.Milestone.Slices {
		for _, task := range slice.Tasks {
			if task.Status != TaskCompleted {
				return false
			}
		}
	}
	return true
}
