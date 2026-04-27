package loop

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SelfImproveConfig configures the self-improvement loop.
type SelfImproveConfig struct {
	MaxIterations       int           // max improvement cycles
	TrajectoryCount     int           // trajectories per iteration
	MinScoreImprovement float64       // minimum score gain to continue
	EvalSamples         int           // examples for evaluation
	CheckpointDir       string        // model checkpoint directory
	CooldownDuration    time.Duration // wait between iterations
}

// SelfImproveLoop orchestrates the collect->train->evaluate->repeat cycle.
type SelfImproveLoop struct {
	config SelfImproveConfig
	logger *slog.Logger

	// Callbacks for each phase — callers wire in the actual implementations.
	Collect  func(ctx context.Context, count int) ([]Trajectory, error)
	Train    func(ctx context.Context, trajectories []Trajectory) error
	Evaluate func(ctx context.Context, samples int) (float64, error)
	Swap     func(ctx context.Context, checkpointPath string) error
}

// NewSelfImproveLoop creates a new self-improvement loop.
func NewSelfImproveLoop(cfg SelfImproveConfig) *SelfImproveLoop {
	return &SelfImproveLoop{
		config: cfg,
		logger: slog.Default(),
	}
}

// Trajectory is a minimal trajectory reference for the loop.
type Trajectory struct {
	Score float64
}

// IterationResult records the outcome of one improvement cycle.
type IterationResult struct {
	Iteration        int
	ScoreBefore      float64
	ScoreAfter       float64
	Improvement      float64
	TrajectoriesUsed int
	Duration         time.Duration
}

// Run executes the self-improvement loop.
func (s *SelfImproveLoop) Run(ctx context.Context) ([]IterationResult, error) {
	if s.Collect == nil || s.Train == nil || s.Evaluate == nil {
		return nil, fmt.Errorf("all callbacks (Collect, Train, Evaluate) must be set")
	}

	var results []IterationResult

	for i := 0; i < s.config.MaxIterations; i++ {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		start := time.Now()
		s.logger.Info("self-improve iteration", "iteration", i+1, "of", s.config.MaxIterations)

		// 1. Evaluate current model.
		scoreBefore, err := s.Evaluate(ctx, s.config.EvalSamples)
		if err != nil {
			return results, fmt.Errorf("evaluate before (iter %d): %w", i+1, err)
		}

		// 2. Collect trajectories.
		trajectories, err := s.Collect(ctx, s.config.TrajectoryCount)
		if err != nil {
			return results, fmt.Errorf("collect (iter %d): %w", i+1, err)
		}

		// Filter to high-reward trajectories only.
		var highReward []Trajectory
		for _, t := range trajectories {
			if t.Score > 0 {
				highReward = append(highReward, t)
			}
		}

		if len(highReward) == 0 {
			s.logger.Warn("no high-reward trajectories, skipping training", "iteration", i+1)
			continue
		}

		// 3. Train on high-reward trajectories.
		if err := s.Train(ctx, highReward); err != nil {
			return results, fmt.Errorf("train (iter %d): %w", i+1, err)
		}

		// 4. Swap in improved model.
		if s.Swap != nil {
			if err := s.Swap(ctx, s.config.CheckpointDir); err != nil {
				return results, fmt.Errorf("swap (iter %d): %w", i+1, err)
			}
		}

		// 5. Evaluate improved model.
		scoreAfter, err := s.Evaluate(ctx, s.config.EvalSamples)
		if err != nil {
			return results, fmt.Errorf("evaluate after (iter %d): %w", i+1, err)
		}

		improvement := scoreAfter - scoreBefore
		result := IterationResult{
			Iteration:        i + 1,
			ScoreBefore:      scoreBefore,
			ScoreAfter:       scoreAfter,
			Improvement:      improvement,
			TrajectoriesUsed: len(highReward),
			Duration:         time.Since(start),
		}
		results = append(results, result)

		s.logger.Info("iteration complete",
			"iteration", i+1,
			"score_before", scoreBefore,
			"score_after", scoreAfter,
			"improvement", improvement,
		)

		// Check if improvement is sufficient.
		if improvement < s.config.MinScoreImprovement {
			s.logger.Info("improvement below threshold, stopping",
				"improvement", improvement,
				"threshold", s.config.MinScoreImprovement,
			)
			break
		}

		// Cooldown.
		if s.config.CooldownDuration > 0 && i < s.config.MaxIterations-1 {
			select {
			case <-time.After(s.config.CooldownDuration):
			case <-ctx.Done():
				return results, ctx.Err()
			}
		}
	}

	return results, nil
}
