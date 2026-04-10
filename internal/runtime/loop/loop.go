package loop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the loop controller's state.
type State int32

const (
	StateStopped State = iota
	StateRunning
	StatePaused
)

// Runner is the function that executes one iteration.
type Runner func(ctx context.Context, iteration int) error

// Controller manages continuous agent execution.
type Controller struct {
	interval time.Duration
	runner   Runner
	state    atomic.Int32
	logger   *slog.Logger

	mu      sync.Mutex
	stopCh  chan struct{}
	pauseCh chan struct{}
}

// NewController creates a new loop controller.
func NewController(interval time.Duration, runner Runner) *Controller {
	c := &Controller{
		interval: interval,
		runner:   runner,
		logger:   slog.Default(),
		stopCh:   make(chan struct{}),
		pauseCh:  make(chan struct{}),
	}
	c.state.Store(int32(StateStopped))
	return c
}

// Start begins the continuous loop.
func (c *Controller) Start(ctx context.Context) error {
	if State(c.state.Load()) == StateRunning {
		return fmt.Errorf("loop already running")
	}
	c.state.Store(int32(StateRunning))

	c.mu.Lock()
	c.stopCh = make(chan struct{})
	stopCh := c.stopCh
	c.mu.Unlock()

	iteration := 0
	for {
		select {
		case <-ctx.Done():
			c.state.Store(int32(StateStopped))
			return ctx.Err()
		case <-stopCh:
			c.state.Store(int32(StateStopped))
			return nil
		default:
		}

		if State(c.state.Load()) == StatePaused {
			select {
			case <-c.pauseCh:
				c.state.Store(int32(StateRunning))
			case <-stopCh:
				c.state.Store(int32(StateStopped))
				return nil
			case <-ctx.Done():
				c.state.Store(int32(StateStopped))
				return ctx.Err()
			}
		}

		iteration++
		c.logger.Info("loop iteration", "iteration", iteration)

		if err := c.runner(ctx, iteration); err != nil {
			c.logger.Error("loop iteration failed", "iteration", iteration, "error", err)
		}

		// Wait for next interval.
		select {
		case <-time.After(c.interval):
		case <-stopCh:
			c.state.Store(int32(StateStopped))
			return nil
		case <-ctx.Done():
			c.state.Store(int32(StateStopped))
			return ctx.Err()
		}
	}
}

// Stop halts the loop.
func (c *Controller) Stop() {
	if State(c.state.Load()) != StateStopped {
		c.mu.Lock()
		select {
		case <-c.stopCh:
			// Already closed.
		default:
			close(c.stopCh)
		}
		c.mu.Unlock()
	}
}

// Pause pauses the loop.
func (c *Controller) Pause() {
	c.state.Store(int32(StatePaused))
}

// Resume resumes a paused loop.
func (c *Controller) Resume() {
	if State(c.state.Load()) == StatePaused {
		select {
		case c.pauseCh <- struct{}{}:
		default:
		}
	}
}

// GetState returns the current state.
func (c *Controller) GetState() State {
	return State(c.state.Load())
}
