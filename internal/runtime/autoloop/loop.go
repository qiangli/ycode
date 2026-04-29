// Package autoloop implements the complete autonomous self-improving agent loop:
// RESEARCH → PLAN → BUILD → EVALUATE → LEARN.
//
// Each phase is pluggable via callback functions. The loop runs until all gaps
// are addressed, the budget is exhausted, stagnation is detected, or the user
// interrupts.
package autoloop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config configures the autonomous loop.
type Config struct {
	Goal            string        // high-level goal (e.g., "improve test coverage to 80%")
	MaxIterations   int           // max research-plan-build-evaluate-learn cycles (default 5)
	Budget          int           // max tokens across all cycles (0 = unlimited)
	CheckCommand    string        // verification command for evaluate phase
	StateDir        string        // persistence directory for loop state
	StagnationLimit int           // stop if eval score unchanged for N iterations (default 2)
	Timeout         time.Duration // overall timeout (0 = no timeout)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		MaxIterations:   5,
		StagnationLimit: 2,
	}
}

// Callbacks holds the pluggable functions for each loop phase.
type Callbacks struct {
	// Research searches for SOTA, memory, and prior work.
	// Returns a gap analysis string.
	Research func(ctx context.Context, goal string) (gapAnalysis string, err error)

	// Plan decomposes gaps into prioritized tasks.
	// Returns task descriptions.
	Plan func(ctx context.Context, goal, gapAnalysis string) (tasks []string, err error)

	// Build executes the tasks (via sprint runner or Ralph loop).
	// Returns number of tasks completed.
	Build func(ctx context.Context, goal string, tasks []string) (completed int, err error)

	// Evaluate runs the eval suite and returns a score.
	Evaluate func(ctx context.Context) (score float64, err error)

	// Learn extracts patterns and persists learnings.
	Learn func(ctx context.Context, iteration int, score float64) error
}

// IterationResult records the outcome of one loop cycle.
type IterationResult struct {
	Iteration     int
	GapAnalysis   string
	TaskCount     int
	TasksComplete int
	ScoreBefore   float64
	ScoreAfter    float64
	Improvement   float64
	Duration      time.Duration
}

// Loop orchestrates the RESEARCH-PLAN-BUILD-EVALUATE-LEARN cycle.
type Loop struct {
	config    *Config
	callbacks *Callbacks
	logger    *slog.Logger
}

// New creates an autonomous loop.
func New(cfg *Config, cb *Callbacks) *Loop {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Loop{
		config:    cfg,
		callbacks: cb,
		logger:    slog.Default(),
	}
}

// Run executes the autonomous loop.
func (l *Loop) Run(ctx context.Context) ([]IterationResult, error) {
	tracer := otel.Tracer("ycode.autoloop")
	ctx, span := tracer.Start(ctx, "ycode.autoloop.run",
		trace.WithAttributes(
			attribute.String("autoloop.goal", l.config.Goal),
			attribute.Int("autoloop.max_iterations", l.config.MaxIterations),
		),
	)
	defer span.End()

	if l.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.config.Timeout)
		defer cancel()
	}

	var results []IterationResult
	var lastScore float64
	stagnationCount := 0

	for i := 1; i <= l.config.MaxIterations; i++ {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		l.logger.Info("autoloop: iteration", "iteration", i, "max", l.config.MaxIterations)
		start := time.Now()

		result := IterationResult{Iteration: i}

		// Phase 1: RESEARCH
		l.logger.Info("autoloop: RESEARCH phase", "iteration", i)
		var gapAnalysis string
		if l.callbacks.Research != nil {
			var err error
			gapAnalysis, err = l.callbacks.Research(ctx, l.config.Goal)
			if err != nil {
				l.logger.Error("autoloop: research failed", "error", err)
				// Continue with empty gap analysis.
			}
		}
		result.GapAnalysis = gapAnalysis

		// Phase 2: PLAN
		l.logger.Info("autoloop: PLAN phase", "iteration", i)
		var tasks []string
		if l.callbacks.Plan != nil {
			var err error
			tasks, err = l.callbacks.Plan(ctx, l.config.Goal, gapAnalysis)
			if err != nil {
				l.logger.Error("autoloop: plan failed", "error", err)
				continue
			}
		}
		result.TaskCount = len(tasks)

		if len(tasks) == 0 {
			l.logger.Info("autoloop: no tasks identified, stopping")
			break
		}

		// Get baseline score before building.
		if l.callbacks.Evaluate != nil {
			result.ScoreBefore, _ = l.callbacks.Evaluate(ctx)
		}

		// Phase 3: BUILD
		l.logger.Info("autoloop: BUILD phase", "iteration", i, "tasks", len(tasks))
		if l.callbacks.Build != nil {
			completed, err := l.callbacks.Build(ctx, l.config.Goal, tasks)
			result.TasksComplete = completed
			if err != nil {
				l.logger.Error("autoloop: build failed", "error", err, "completed", completed)
			}
		}

		// Phase 4: EVALUATE
		l.logger.Info("autoloop: EVALUATE phase", "iteration", i)
		if l.callbacks.Evaluate != nil {
			score, err := l.callbacks.Evaluate(ctx)
			if err != nil {
				l.logger.Error("autoloop: evaluate failed", "error", err)
			}
			result.ScoreAfter = score
			result.Improvement = score - result.ScoreBefore
		}

		// Phase 5: LEARN
		l.logger.Info("autoloop: LEARN phase", "iteration", i)
		if l.callbacks.Learn != nil {
			if err := l.callbacks.Learn(ctx, i, result.ScoreAfter); err != nil {
				l.logger.Warn("autoloop: learn failed", "error", err)
			}
		}

		result.Duration = time.Since(start)
		results = append(results, result)

		l.logger.Info("autoloop: iteration complete",
			"iteration", i,
			"tasks", result.TaskCount,
			"completed", result.TasksComplete,
			"score_before", result.ScoreBefore,
			"score_after", result.ScoreAfter,
			"improvement", result.Improvement,
			"duration", result.Duration,
		)

		// Stagnation detection.
		if result.ScoreAfter == lastScore {
			stagnationCount++
		} else {
			stagnationCount = 0
		}
		lastScore = result.ScoreAfter

		if l.config.StagnationLimit > 0 && stagnationCount >= l.config.StagnationLimit {
			l.logger.Info("autoloop: stagnation detected", "unchanged_iterations", stagnationCount)
			break
		}
	}

	l.logger.Info("autoloop: complete",
		"iterations", len(results),
		"final_score", lastScore,
	)

	return results, nil
}

// FormatSummary creates a human-readable summary of loop results.
func FormatSummary(results []IterationResult) string {
	if len(results) == 0 {
		return "No iterations completed."
	}

	summary := fmt.Sprintf("Autonomous Loop: %d iterations\n", len(results))
	for _, r := range results {
		summary += fmt.Sprintf("  Iteration %d: %d/%d tasks | score %.2f → %.2f (%+.2f) | %s\n",
			r.Iteration,
			r.TasksComplete, r.TaskCount,
			r.ScoreBefore, r.ScoreAfter, r.Improvement,
			r.Duration.Round(time.Second),
		)
	}

	first := results[0]
	last := results[len(results)-1]
	summary += fmt.Sprintf("  Total improvement: %.2f → %.2f (%+.2f)\n",
		first.ScoreBefore, last.ScoreAfter, last.ScoreAfter-first.ScoreBefore,
	)
	return summary
}
