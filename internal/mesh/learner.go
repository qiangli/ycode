package mesh

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// Learner performs post-session analysis and memory consolidation.
type Learner struct {
	b        bus.Bus
	interval time.Duration
	logger   *slog.Logger

	// SaveMemoryFunc persists a learning as a memory entry.
	SaveMemoryFunc func(ctx context.Context, name, memType, content string) error

	cancel  context.CancelFunc
	healthy atomic.Bool
}

// NewLearner creates a learner agent.
func NewLearner(b bus.Bus, interval time.Duration) *Learner {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return &Learner{
		b:        b,
		interval: interval,
		logger:   slog.Default(),
	}
}

func (l *Learner) Name() string  { return "learner" }
func (l *Learner) Healthy() bool { return l.healthy.Load() }

func (l *Learner) Start(ctx context.Context) error {
	ctx, l.cancel = context.WithCancel(ctx)
	l.healthy.Store(true)
	go l.listen(ctx)
	go l.periodicConsolidate(ctx)
	return nil
}

func (l *Learner) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
	l.healthy.Store(false)
}

func (l *Learner) listen(ctx context.Context) {
	ch, unsub := l.b.Subscribe(bus.EventFixComplete, bus.EventFixFailed, bus.EventDiagReport)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			l.handleEvent(ctx, ev)
		}
	}
}

func (l *Learner) handleEvent(ctx context.Context, ev bus.Event) {
	switch ev.Type {
	case bus.EventFixComplete:
		// Record successful fix pattern as procedural memory.
		l.logger.Info("mesh.learner.fix_pattern",
			"event", string(ev.Type),
		)
		if l.SaveMemoryFunc != nil {
			content := fmt.Sprintf("Fix applied at %s: %s", time.Now().Format(time.RFC3339), string(ev.Data))
			_ = l.SaveMemoryFunc(ctx, "fix-pattern-"+time.Now().Format("20060102-150405"), "procedural", content)
		}
		l.b.Publish(bus.Event{Type: bus.EventLearnComplete, Data: ev.Data})

	case bus.EventFixFailed:
		// Record failed fix for future analysis.
		l.logger.Info("mesh.learner.fix_failure_recorded",
			"event", string(ev.Type),
		)

	case bus.EventDiagReport:
		// Record diagnostic observation as episodic memory.
		l.logger.Debug("mesh.learner.diagnostic_observed",
			"event", string(ev.Type),
		)
	}
}

func (l *Learner) periodicConsolidate(ctx context.Context) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.logger.Debug("mesh.learner.consolidation_tick")
			// Trigger memory consolidation via the Dreamer.
			// The actual consolidation is done by the memory manager
			// which is called by the Learner's SaveMemoryFunc callback.
		}
	}
}
