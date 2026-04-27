package ralph

import (
	"context"
	"testing"
	"time"
)

func TestRunMaxIterations(t *testing.T) {
	cfg := &Config{
		MaxIterations:   3,
		StagnationLimit: 0, // disable
	}

	var iterations int
	step := func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		iterations++
		return "output", float64(iteration), nil
	}

	ctrl := NewController(cfg, step)
	err := ctrl.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if iterations != 3 {
		t.Fatalf("iterations = %d, want 3", iterations)
	}
	if ctrl.GetState().Status != "max_iterations" {
		t.Fatalf("status = %q, want max_iterations", ctrl.GetState().Status)
	}
}

func TestRunTargetScoreReached(t *testing.T) {
	cfg := &Config{
		MaxIterations:   10,
		TargetScore:     5.0,
		StagnationLimit: 0,
	}

	step := func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		return "output", float64(iteration) * 2, nil
	}

	ctrl := NewController(cfg, step)
	err := ctrl.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ctrl.GetState().Status != "target_reached" {
		t.Fatalf("status = %q, want target_reached", ctrl.GetState().Status)
	}
	// Score of 6.0 (iteration 3) should trigger >= 5.0.
	if ctrl.GetState().Iteration > 5 {
		t.Fatalf("should stop early, iteration = %d", ctrl.GetState().Iteration)
	}
}

func TestRunStagnationDetection(t *testing.T) {
	cfg := &Config{
		MaxIterations:   20,
		StagnationLimit: 3,
	}

	step := func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		// Return same score every iteration.
		return "output", 1.0, nil
	}

	ctrl := NewController(cfg, step)
	err := ctrl.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ctrl.GetState().Status != "stagnated" {
		t.Fatalf("status = %q, want stagnated", ctrl.GetState().Status)
	}
	if ctrl.GetState().Iteration > 5 {
		t.Fatalf("should stop early due to stagnation, iteration = %d", ctrl.GetState().Iteration)
	}
}

func TestRunContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	cfg := &Config{
		MaxIterations:   100,
		StagnationLimit: 0,
	}

	step := func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		time.Sleep(10 * time.Millisecond)
		return "output", 0, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ctrl := NewController(cfg, step)
	err := ctrl.Run(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
	if ctrl.GetState().Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", ctrl.GetState().Status)
	}
}

func TestRunWithCheckAndCommit(t *testing.T) {
	cfg := &Config{
		MaxIterations:   2,
		CommitOnSuccess: true,
		CommitMessage:   "test commit",
		StagnationLimit: 0,
	}

	step := func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		return "output", float64(iteration), nil
	}

	var checkCalls, commitCalls int
	ctrl := NewController(cfg, step)
	ctrl.SetCheck(func(ctx context.Context) (bool, string, error) {
		checkCalls++
		return true, "ok", nil
	})
	ctrl.SetCommit(func(ctx context.Context, message string) error {
		commitCalls++
		if message != "test commit" {
			t.Fatalf("commit message = %q", message)
		}
		return nil
	})

	err := ctrl.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if checkCalls != 2 {
		t.Fatalf("check calls = %d, want 2", checkCalls)
	}
	if commitCalls != 2 {
		t.Fatalf("commit calls = %d, want 2", commitCalls)
	}
	if len(ctrl.GetState().Commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(ctrl.GetState().Commits))
	}
}

func TestRunDefaultConfig(t *testing.T) {
	ctrl := NewController(nil, func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		return "", float64(iteration), nil
	})
	err := ctrl.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ctrl.GetState().Status != "max_iterations" {
		t.Fatalf("status = %q, want max_iterations", ctrl.GetState().Status)
	}
	if ctrl.GetState().Iteration != 10 {
		t.Fatalf("iterations = %d, want 10", ctrl.GetState().Iteration)
	}
}
