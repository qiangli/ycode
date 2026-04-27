// Package ralph implements the Ralph Loop — a tight iterative agent loop
// for autonomous software development: step → check → commit → repeat.
//
// Named after the "Ralph Wiggum loop" pattern popularized by Boris Cherny
// (Anthropic): agent runs in a tight loop with file-based persistent state,
// eval-driven termination, and automatic checkpointing.
package ralph

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config controls the Ralph loop behavior.
type Config struct {
	MaxIterations   int           // hard stop (default 10)
	TargetScore     float64       // eval score to stop (0 = disabled)
	CheckCommand    string        // command to run after each step (e.g., "go test ./...")
	CommitOnSuccess bool          // auto-commit when check passes
	CommitMessage   string        // commit message template
	StagnationLimit int           // stop if score unchanged for N iterations (default 3)
	Timeout         time.Duration // overall timeout (0 = no timeout)
	FreshContext    bool          // spawn each iteration with clean context (only state+progress injected)
	PRDPath         string        // path to prd.json (optional — if set, enables story-driven mode)
	ProgressDir     string        // directory for progress.txt (optional)
	ArchiveDir      string        // directory for archived runs (optional)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		MaxIterations:   10,
		StagnationLimit: 3,
	}
}

// StepFunc is the function that performs one iteration of work.
// It receives the current state and returns the output and optional score.
type StepFunc func(ctx context.Context, state *State, iteration int) (output string, score float64, err error)

// CheckFunc runs the verification step (e.g., test suite).
// Returns true if the check passed.
type CheckFunc func(ctx context.Context) (passed bool, output string, err error)

// CommitFunc performs a git commit.
type CommitFunc func(ctx context.Context, message string) error

// Controller manages the Ralph loop lifecycle.
type Controller struct {
	config *Config
	step   StepFunc
	check  CheckFunc
	commit CommitFunc
	logger *slog.Logger
	state  *State
}

// NewController creates a new Ralph loop controller.
func NewController(cfg *Config, step StepFunc) *Controller {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Controller{
		config: cfg,
		step:   step,
		logger: slog.Default(),
		state:  NewState(),
	}
}

// SetCheck sets the verification function.
func (c *Controller) SetCheck(fn CheckFunc) { c.check = fn }

// SetCommit sets the commit function.
func (c *Controller) SetCommit(fn CommitFunc) { c.commit = fn }

// SetLogger sets the logger.
func (c *Controller) SetLogger(l *slog.Logger) { c.logger = l }

// GetState returns the current state.
func (c *Controller) GetState() *State { return c.state }

// Run executes the Ralph loop.
func (c *Controller) Run(ctx context.Context) error {
	cfg := c.config

	tracer := otel.Tracer("ycode.ralph")
	ctx, span := tracer.Start(ctx, "ycode.ralph.run",
		trace.WithAttributes(
			attribute.Int("ralph.max_iterations", cfg.MaxIterations),
			attribute.Float64("ralph.target_score", cfg.TargetScore),
			attribute.Bool("ralph.fresh_context", cfg.FreshContext),
			attribute.String("ralph.prd_path", cfg.PRDPath),
		))
	defer span.End()

	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	// Load PRD if configured.
	var prd *PRD
	var progressLog *ProgressLog
	if cfg.PRDPath != "" {
		var err error
		prd, err = LoadPRD(cfg.PRDPath)
		if err != nil {
			c.logger.Warn("ralph: failed to load PRD, running without story tracking", "error", err)
		}
	}
	if cfg.ProgressDir != "" {
		progressLog = NewProgressLog(filepath.Join(cfg.ProgressDir, "progress.txt"))
	}

	stagnationCount := 0
	lastScore := -1.0

	for i := 1; i <= cfg.MaxIterations; i++ {
		select {
		case <-ctx.Done():
			c.state.Status = "cancelled"
			return ctx.Err()
		default:
		}

		c.state.Iteration = i
		c.state.Status = "running"

		_, iterSpan := tracer.Start(ctx, "ycode.ralph.iteration",
			trace.WithAttributes(
				attribute.Int("ralph.iteration", i),
			))

		c.logger.Info("ralph: iteration", "iteration", i, "max", cfg.MaxIterations)

		// Pick current story from PRD.
		var currentStory *Story
		if prd != nil {
			currentStory = prd.NextStory()
			if currentStory == nil {
				c.logger.Info("ralph: all stories pass")
				c.state.Status = "target_reached"
				return nil
			}
			c.logger.Info("ralph: working on story", "id", currentStory.ID, "title", currentStory.Title)
		}

		// Step: perform one iteration of work.
		output, score, err := c.step(ctx, c.state, i)
		if err != nil {
			c.state.LastError = err.Error()
			c.logger.Error("ralph: step failed", "iteration", i, "error", err)
			iterSpan.RecordError(err)
			iterSpan.End()
			continue
		}

		c.state.LastOutput = output
		c.state.LastScore = score

		// Check: verify the output.
		checkPassed := true
		if c.check != nil {
			passed, checkOutput, checkErr := c.check(ctx)
			if checkErr != nil {
				c.logger.Error("ralph: check error", "iteration", i, "error", checkErr)
				checkPassed = false
			} else {
				checkPassed = passed
				c.state.LastCheckOutput = checkOutput
			}
		}

		// Update PRD and progress log after check.
		if prd != nil && currentStory != nil && checkPassed {
			prd.UpdateStory(currentStory.ID, true, fmt.Sprintf("Iteration %d: passed", i))
		}
		if progressLog != nil {
			storyID := ""
			if currentStory != nil {
				storyID = currentStory.ID
			}
			progressLog.Append(i, storyID, "step", output, "")
		}

		// Commit if check passed and auto-commit is enabled.
		if checkPassed && cfg.CommitOnSuccess && c.commit != nil {
			msg := cfg.CommitMessage
			if msg == "" {
				msg = fmt.Sprintf("ralph: iteration %d", i)
			}
			if err := c.commit(ctx, msg); err != nil {
				c.logger.Error("ralph: commit failed", "iteration", i, "error", err)
			} else {
				c.state.Commits = append(c.state.Commits, msg)
			}
		}

		// End iteration span with attributes.
		iterSpan.SetAttributes(
			attribute.Float64("ralph.score", score),
			attribute.Bool("ralph.check_passed", checkPassed),
		)
		iterSpan.End()

		// Check termination conditions.
		if cfg.TargetScore > 0 && score >= cfg.TargetScore {
			c.logger.Info("ralph: target score reached", "score", score, "target", cfg.TargetScore)
			c.state.Status = "target_reached"
			return nil
		}

		// Stagnation detection.
		if score == lastScore {
			stagnationCount++
		} else {
			stagnationCount = 0
		}
		lastScore = score

		if cfg.StagnationLimit > 0 && stagnationCount >= cfg.StagnationLimit {
			c.logger.Info("ralph: stagnation detected", "unchanged_iterations", stagnationCount)
			c.state.Status = "stagnated"
			return nil
		}
	}

	// Archive if PRD is set and all stories pass.
	if prd != nil && prd.AllPass() && cfg.ArchiveDir != "" {
		if cfg.ProgressDir != "" {
			ArchiveRun(cfg.ProgressDir, cfg.ArchiveDir, prd.Feature)
		}
	}

	c.state.Status = "max_iterations"
	return nil
}
