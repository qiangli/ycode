package sprint

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/runtime/ralph"
)

// RunnerConfig configures the sprint runner.
type RunnerConfig struct {
	// RalphDeps provides the runtime dependencies for executing tasks.
	RalphDeps *ralph.RuntimeDeps

	// CheckCommand is the verification command (e.g., "go test ./...").
	CheckCommand string

	// StateDir is the directory for sprint state persistence.
	StateDir string

	// MaxTaskAttempts is the maximum retries per task (default 2).
	MaxTaskAttempts int

	Logger *slog.Logger
}

// Runner executes a sprint: iterates through tasks, runs each with a fresh
// Ralph iteration, applies two-stage review, and tracks progress.
type Runner struct {
	config *RunnerConfig
	state  *SprintState
	logger *slog.Logger
}

// NewRunner creates a sprint runner.
func NewRunner(cfg *RunnerConfig, state *SprintState) *Runner {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxTaskAttempts <= 0 {
		cfg.MaxTaskAttempts = 2
	}
	return &Runner{
		config: cfg,
		state:  state,
		logger: logger,
	}
}

// Run executes the sprint state machine.
func (r *Runner) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			r.state.Phase = PhaseFailed
			_ = r.state.Save(r.config.StateDir)
			return ctx.Err()
		default:
		}

		if r.state.BudgetExceeded() {
			r.logger.Warn("sprint: budget exceeded", "used", r.state.TokensUsed, "budget", r.state.Budget)
			r.state.Phase = PhaseFailed
			_ = r.state.Save(r.config.StateDir)
			return fmt.Errorf("token budget exceeded: %d/%d", r.state.TokensUsed, r.state.Budget)
		}

		switch r.state.Phase {
		case PhasePlan:
			// Planning is done by the caller before Runner.Run().
			// Transition to execute.
			r.state.Phase = PhaseExecute
			_ = r.state.Save(r.config.StateDir)

		case PhaseExecute:
			task := r.state.CurrentTaskRef()
			if task == nil {
				// All tasks executed.
				r.state.Phase = PhaseComplete
				_ = r.state.Save(r.config.StateDir)
				continue
			}

			r.logger.Info("sprint: executing task",
				"task", task.ID,
				"description", task.Description,
				"slice", task.SliceID,
			)

			if err := r.executeTask(ctx, task); err != nil {
				r.logger.Error("sprint: task failed", "task", task.ID, "error", err)
				task.Status = TaskFailed
				task.Attempts++

				if task.Attempts >= r.config.MaxTaskAttempts {
					r.logger.Warn("sprint: max attempts reached, skipping task", "task", task.ID)
					if !r.state.AdvanceTask() {
						r.state.Phase = PhaseComplete
					}
				}
			} else {
				task.Status = TaskCompleted
				if !r.state.AdvanceTask() {
					r.state.Phase = PhaseComplete
				}
			}
			_ = r.state.Save(r.config.StateDir)

		case PhaseComplete:
			r.logger.Info("sprint: all tasks complete, moving to reassess")
			r.state.Phase = PhaseReassess
			_ = r.state.Save(r.config.StateDir)

		case PhaseReassess:
			// Check if all tasks passed and milestone is met.
			if r.state.AllTasksComplete() {
				r.state.Phase = PhaseValidateMilestone
			} else {
				// Some tasks failed — could re-plan here.
				r.state.Phase = PhaseFailed
			}
			_ = r.state.Save(r.config.StateDir)

		case PhaseValidateMilestone:
			r.logger.Info("sprint: milestone validation")
			r.state.Phase = PhaseDone
			_ = r.state.Save(r.config.StateDir)
			return nil

		case PhaseDone:
			return nil

		case PhaseFailed:
			return fmt.Errorf("sprint failed in phase %s", r.state.Phase)

		default:
			return fmt.Errorf("unknown sprint phase: %s", r.state.Phase)
		}
	}
}

// executeTask runs a single task using a Ralph iteration with fresh context.
func (r *Runner) executeTask(ctx context.Context, task *SprintTask) error {
	task.Status = TaskRunning

	// Build task prompt with context.
	prompt := fmt.Sprintf(`## Sprint Task: %s

**Description**: %s

`, task.ID, task.Description)

	if len(task.AcceptanceCriteria) > 0 {
		prompt += "**Acceptance Criteria**:\n"
		for _, ac := range task.AcceptanceCriteria {
			prompt += fmt.Sprintf("- %s\n", ac)
		}
		prompt += "\n"
	}

	if task.ReviewFeedback != "" {
		prompt += fmt.Sprintf("**Previous review feedback**: %s\n\n", task.ReviewFeedback)
	}

	prompt += "Implement this task. Write the code, create/modify files as needed, and run tests to verify."

	// Configure a single Ralph iteration for this task.
	cfg := ralph.DefaultConfig()
	cfg.MaxIterations = 1
	cfg.FreshContext = true

	stepFunc := ralph.NewRuntimeStepFunc(&ralph.RuntimeStepConfig{
		Deps:       r.config.RalphDeps,
		UserPrompt: prompt,
	})

	ctrl := ralph.NewController(cfg, stepFunc)

	if r.config.CheckCommand != "" {
		ctrl.SetCheck(ralph.NewBashCheckFunc(r.config.CheckCommand))
	}

	start := time.Now()
	err := ctrl.Run(ctx)
	elapsed := time.Since(start)

	task.Output = ctrl.GetState().LastOutput
	r.logger.Info("sprint: task completed",
		"task", task.ID,
		"duration", elapsed,
		"status", ctrl.GetState().Status,
	)

	return err
}
