package autoloop

import (
	"context"
	"testing"
)

func TestLoopBasicCycle(t *testing.T) {
	iteration := 0
	cb := &Callbacks{
		Research: func(ctx context.Context, goal string) (string, error) {
			return "gap: missing tests", nil
		},
		Plan: func(ctx context.Context, goal, gaps string) ([]string, error) {
			if iteration >= 2 {
				return nil, nil // no more tasks after 2 iterations
			}
			return []string{"add tests"}, nil
		},
		Build: func(ctx context.Context, goal string, tasks []string) (int, error) {
			iteration++
			return len(tasks), nil
		},
		Evaluate: func(ctx context.Context) (float64, error) {
			return float64(iteration) * 0.3, nil
		},
		Learn: func(ctx context.Context, iter int, score float64) error {
			return nil
		},
	}

	cfg := &Config{
		Goal:            "improve coverage",
		MaxIterations:   5,
		StagnationLimit: 3,
	}

	loop := New(cfg, cb)
	results, err := loop.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("iterations = %d, want 2", len(results))
	}

	if results[0].TasksComplete != 1 {
		t.Fatalf("first iteration tasks = %d, want 1", results[0].TasksComplete)
	}
}

func TestLoopStagnation(t *testing.T) {
	cb := &Callbacks{
		Research: func(ctx context.Context, goal string) (string, error) {
			return "gap", nil
		},
		Plan: func(ctx context.Context, goal, gaps string) ([]string, error) {
			return []string{"task"}, nil
		},
		Build: func(ctx context.Context, goal string, tasks []string) (int, error) {
			return 1, nil
		},
		Evaluate: func(ctx context.Context) (float64, error) {
			return 0.5, nil // always same score
		},
	}

	cfg := &Config{
		Goal:            "test stagnation",
		MaxIterations:   10,
		StagnationLimit: 2,
	}

	loop := New(cfg, cb)
	results, err := loop.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should stop after stagnation limit + 1 (first non-stagnant + N stagnant).
	if len(results) > 4 {
		t.Fatalf("expected early stop due to stagnation, got %d iterations", len(results))
	}
}

func TestLoopContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())

	cb := &Callbacks{
		Research: func(ctx context.Context, goal string) (string, error) {
			cancel() // cancel immediately
			return "", nil
		},
		Plan: func(ctx context.Context, goal, gaps string) ([]string, error) {
			return []string{"task"}, nil
		},
	}

	cfg := &Config{Goal: "test cancel", MaxIterations: 10}
	loop := New(cfg, cb)
	_, err := loop.Run(ctx)
	if err == nil {
		t.Fatal("expected error from cancellation")
	}
}

func TestFormatSummary(t *testing.T) {
	results := []IterationResult{
		{Iteration: 1, TaskCount: 3, TasksComplete: 2, ScoreBefore: 0.5, ScoreAfter: 0.7, Improvement: 0.2},
		{Iteration: 2, TaskCount: 2, TasksComplete: 2, ScoreBefore: 0.7, ScoreAfter: 0.85, Improvement: 0.15},
	}

	summary := FormatSummary(results)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestFormatSummaryEmpty(t *testing.T) {
	summary := FormatSummary(nil)
	if summary != "No iterations completed." {
		t.Fatalf("unexpected summary: %s", summary)
	}
}
