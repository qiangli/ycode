package loop

import (
	"context"
	"fmt"
	"testing"
)

func TestSelfImproveLoop_MissingCallbacks(t *testing.T) {
	loop := NewSelfImproveLoop(SelfImproveConfig{MaxIterations: 1})
	_, err := loop.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when callbacks are nil")
	}
}

func TestSelfImproveLoop_StopsOnThreshold(t *testing.T) {
	evalCall := 0
	loop := NewSelfImproveLoop(SelfImproveConfig{
		MaxIterations:       5,
		TrajectoryCount:     10,
		MinScoreImprovement: 0.1,
		EvalSamples:         5,
	})

	// Scores: before=0.5, after=0.55 -> improvement 0.05 < 0.1 threshold -> stop after 1 iteration.
	loop.Evaluate = func(_ context.Context, _ int) (float64, error) {
		evalCall++
		if evalCall == 1 {
			return 0.5, nil
		}
		return 0.55, nil
	}
	loop.Collect = func(_ context.Context, count int) ([]Trajectory, error) {
		trajs := make([]Trajectory, count)
		for i := range trajs {
			trajs[i] = Trajectory{Score: 0.8}
		}
		return trajs, nil
	}
	loop.Train = func(_ context.Context, _ []Trajectory) error {
		return nil
	}

	results, err := loop.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 iteration result, got %d", len(results))
	}
	if results[0].Improvement < 0.049 || results[0].Improvement > 0.051 {
		t.Errorf("improvement = %f, want ~0.05", results[0].Improvement)
	}
}

func TestSelfImproveLoop_MultipleIterations(t *testing.T) {
	evalCall := 0
	loop := NewSelfImproveLoop(SelfImproveConfig{
		MaxIterations:       3,
		TrajectoryCount:     5,
		MinScoreImprovement: 0.05,
		EvalSamples:         5,
	})

	// Each iteration improves by 0.1 (above 0.05 threshold).
	// iter1: before=0.3, after=0.4  -> 0.1
	// iter2: before=0.4, after=0.5  -> 0.1
	// iter3: before=0.5, after=0.6  -> 0.1
	scores := []float64{0.3, 0.4, 0.4, 0.5, 0.5, 0.6}
	loop.Evaluate = func(_ context.Context, _ int) (float64, error) {
		s := scores[evalCall]
		evalCall++
		return s, nil
	}
	loop.Collect = func(_ context.Context, count int) ([]Trajectory, error) {
		return []Trajectory{{Score: 1.0}, {Score: 0.5}}, nil
	}
	loop.Train = func(_ context.Context, _ []Trajectory) error { return nil }

	results, err := loop.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 iteration results, got %d", len(results))
	}
}

func TestSelfImproveLoop_SkipsWhenNoHighReward(t *testing.T) {
	evalCall := 0
	trainCalled := false
	loop := NewSelfImproveLoop(SelfImproveConfig{
		MaxIterations:       1,
		TrajectoryCount:     5,
		MinScoreImprovement: 0.0,
		EvalSamples:         5,
	})

	loop.Evaluate = func(_ context.Context, _ int) (float64, error) {
		evalCall++
		return 0.5, nil
	}
	// All trajectories have score <= 0 -> no high-reward -> skip training.
	loop.Collect = func(_ context.Context, _ int) ([]Trajectory, error) {
		return []Trajectory{{Score: 0}, {Score: -1}}, nil
	}
	loop.Train = func(_ context.Context, _ []Trajectory) error {
		trainCalled = true
		return nil
	}

	results, err := loop.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if trainCalled {
		t.Error("Train should not have been called when no high-reward trajectories")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (skipped), got %d", len(results))
	}
}

func TestSelfImproveLoop_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	loop := NewSelfImproveLoop(SelfImproveConfig{MaxIterations: 5})
	loop.Collect = func(_ context.Context, _ int) ([]Trajectory, error) { return nil, nil }
	loop.Train = func(_ context.Context, _ []Trajectory) error { return nil }
	loop.Evaluate = func(_ context.Context, _ int) (float64, error) { return 0, nil }

	results, err := loop.Run(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSelfImproveLoop_CollectError(t *testing.T) {
	loop := NewSelfImproveLoop(SelfImproveConfig{MaxIterations: 1, EvalSamples: 1, TrajectoryCount: 1})
	loop.Evaluate = func(_ context.Context, _ int) (float64, error) { return 0.5, nil }
	loop.Collect = func(_ context.Context, _ int) ([]Trajectory, error) {
		return nil, fmt.Errorf("collection failed")
	}
	loop.Train = func(_ context.Context, _ []Trajectory) error { return nil }

	_, err := loop.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Collect")
	}
}
