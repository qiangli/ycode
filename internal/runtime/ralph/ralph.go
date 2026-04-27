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
	"time"
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

	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
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
		c.logger.Info("ralph: iteration", "iteration", i, "max", cfg.MaxIterations)

		// Step: perform one iteration of work.
		output, score, err := c.step(ctx, c.state, i)
		if err != nil {
			c.state.LastError = err.Error()
			c.logger.Error("ralph: step failed", "iteration", i, "error", err)
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

	c.state.Status = "max_iterations"
	return nil
}
