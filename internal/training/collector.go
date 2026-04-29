package training

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/training/rollout"
)

// Collector gathers trajectories from successful agent sessions.
// Trajectories are scored using the configured reward function and
// persisted as JSONL for later training.
type Collector struct {
	outputPath string
	logger     *slog.Logger
}

// NewCollector creates a trajectory collector.
func NewCollector(outputPath string) *Collector {
	return &Collector{
		outputPath: outputPath,
		logger:     slog.Default(),
	}
}

// CollectFromSession converts a completed session into a scored trajectory.
func (c *Collector) CollectFromSession(
	taskName string,
	messages []rollout.Message,
	score float64,
	turnsUsed int,
	toolErrors int,
	duration time.Duration,
	finished bool,
) (*rollout.ScoredTrajectory, error) {
	traj := &rollout.ScoredTrajectory{
		ID:         fmt.Sprintf("traj_%d", time.Now().UnixNano()),
		TaskName:   taskName,
		Messages:   messages,
		Score:      score,
		TurnsUsed:  turnsUsed,
		ToolErrors: toolErrors,
		Duration:   duration,
		Finished:   finished,
	}

	c.logger.Info("training.collector: collected trajectory",
		"task", taskName,
		"score", score,
		"turns", turnsUsed,
		"errors", toolErrors,
	)

	return traj, nil
}

// Save persists trajectories to JSONL file.
func (c *Collector) Save(trajectories []rollout.ScoredTrajectory) error {
	return rollout.SaveTrajectories(c.outputPath, trajectories)
}

// Load reads trajectories from JSONL file.
func (c *Collector) Load() ([]rollout.ScoredTrajectory, error) {
	return rollout.LoadTrajectories(c.outputPath)
}

// WireSelfImproveCallbacks connects the collector and trainer to the
// self-improvement loop callbacks.
type TrainingDeps struct {
	// TrajectoryPath is the JSONL file for trajectory storage.
	TrajectoryPath string

	// Collector gathers trajectories.
	Collector *Collector

	// TrainFunc runs a training step on trajectories.
	// In production, this calls the GRPO Python subprocess.
	TrainFunc func(ctx context.Context, trajectories []rollout.ScoredTrajectory) error

	// EvalFunc runs the evaluation suite and returns a score.
	EvalFunc func(ctx context.Context, samples int) (float64, error)

	// SwapFunc reloads the model with updated weights.
	SwapFunc func(ctx context.Context, checkpointPath string) error
}

// WireCallbacks returns callback functions for the self-improvement loop.
func WireCallbacks(deps *TrainingDeps) (
	collect func(ctx context.Context, count int) ([]Trajectory, error),
	train func(ctx context.Context, trajectories []Trajectory) error,
	evaluate func(ctx context.Context, samples int) (float64, error),
	swap func(ctx context.Context, checkpointPath string) error,
) {
	collect = func(ctx context.Context, count int) ([]Trajectory, error) {
		if deps.Collector == nil {
			return nil, fmt.Errorf("collector not configured")
		}
		scored, err := deps.Collector.Load()
		if err != nil {
			return nil, err
		}
		// Convert to loop.Trajectory (minimal type).
		result := make([]Trajectory, 0, len(scored))
		for _, s := range scored {
			if len(result) >= count {
				break
			}
			result = append(result, Trajectory{Score: s.Score})
		}
		return result, nil
	}

	train = func(ctx context.Context, trajectories []Trajectory) error {
		if deps.TrainFunc == nil {
			slog.Info("training: no train function configured, skipping")
			return nil
		}
		// Load full trajectories for training.
		scored, err := deps.Collector.Load()
		if err != nil {
			return err
		}
		return deps.TrainFunc(ctx, scored)
	}

	evaluate = deps.EvalFunc
	swap = deps.SwapFunc

	return
}

// Trajectory is the minimal trajectory type used by the self-improvement loop.
type Trajectory struct {
	Score float64
}
