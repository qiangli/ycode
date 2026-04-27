package mesh

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// Trainer orchestrates scheduled model training.
type Trainer struct {
	b        bus.Bus
	interval time.Duration
	logger   *slog.Logger

	// TrainFunc runs the training pipeline.
	// Receives context and returns whether improvement was achieved.
	TrainFunc func(ctx context.Context) (improved bool, score float64, err error)

	cancel  context.CancelFunc
	healthy atomic.Bool
}

// NewTrainer creates a trainer agent.
func NewTrainer(b bus.Bus, interval time.Duration) *Trainer {
	if interval <= 0 {
		interval = 24 * time.Hour // nightly default
	}
	return &Trainer{
		b:        b,
		interval: interval,
		logger:   slog.Default(),
	}
}

func (t *Trainer) Name() string  { return "trainer" }
func (t *Trainer) Healthy() bool { return t.healthy.Load() }

func (t *Trainer) Start(ctx context.Context) error {
	ctx, t.cancel = context.WithCancel(ctx)
	t.healthy.Store(true)
	go t.schedule(ctx)
	return nil
}

func (t *Trainer) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	t.healthy.Store(false)
}

func (t *Trainer) schedule(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.runTraining(ctx)
		}
	}
}

func (t *Trainer) runTraining(ctx context.Context) {
	if t.TrainFunc == nil {
		t.logger.Debug("mesh.trainer.no_train_func")
		return
	}

	t.logger.Info("mesh.trainer.starting")
	t.b.Publish(bus.Event{Type: bus.EventTrainStart})

	start := time.Now()
	improved, score, err := t.TrainFunc(ctx)
	duration := time.Since(start)

	if err != nil {
		t.logger.Error("mesh.trainer.error", "error", err, "duration", duration)
		return
	}

	t.logger.Info("mesh.trainer.complete",
		"improved", improved,
		"score", score,
		"duration", duration,
	)
	t.b.Publish(bus.Event{
		Type: bus.EventTrainComplete,
		Data: mustMarshal(map[string]any{
			"improved": improved,
			"score":    score,
			"duration": duration.String(),
		}),
	})
}

// RunNow triggers an immediate training run.
func (t *Trainer) RunNow(ctx context.Context) {
	t.runTraining(ctx)
}
