package memory

import (
	"context"
	"log/slog"
	"time"
)

// DefaultSweepInterval is the default interval between TTL sweep runs.
const DefaultSweepInterval = 15 * time.Minute

// Sweeper removes expired memories in the background.
// Inspired by LangGraph's store sweep_interval_minutes pattern.
type Sweeper struct {
	manager  *Manager
	interval time.Duration
	cancel   context.CancelFunc
}

// NewSweeper creates a background sweeper for the given manager.
// Call Start() to begin the sweep loop.
func NewSweeper(manager *Manager, interval time.Duration) *Sweeper {
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	return &Sweeper{
		manager:  manager,
		interval: interval,
	}
}

// Start begins the background sweep loop. It is safe to call multiple times;
// subsequent calls are no-ops if the sweeper is already running.
func (s *Sweeper) Start() {
	if s.cancel != nil {
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.loop(ctx)
}

// Stop halts the background sweep loop.
func (s *Sweeper) Stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *Sweeper) loop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run an initial sweep immediately.
	s.sweep()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep()
		}
	}
}

func (s *Sweeper) sweep() {
	now := time.Now()
	removed := 0

	memories, err := s.manager.All()
	if err != nil {
		slog.Warn("memory sweep: failed to list memories", "error", err)
		return
	}

	for _, mem := range memories {
		if mem.ValidUntil != nil && mem.ValidUntil.Before(now) {
			if err := s.manager.Forget(mem.Name); err != nil {
				slog.Warn("memory sweep: failed to delete expired memory",
					"name", mem.Name, "expired_at", mem.ValidUntil, "error", err)
				continue
			}
			removed++
			slog.Info("memory sweep: removed expired memory",
				"name", mem.Name, "expired_at", mem.ValidUntil)
		}
	}

	if removed > 0 {
		slog.Info("memory sweep complete", "removed", removed)
	}
}

// SweepOnce runs a single sweep pass (useful for testing).
func (s *Sweeper) SweepOnce() {
	s.sweep()
}
