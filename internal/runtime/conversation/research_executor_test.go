package conversation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestExecuteNoDependencies(t *testing.T) {
	plan := NewResearchPlanV2("test")
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.AddTask("t2", "q2", "Plan", nil)
	plan.AddTask("t3", "q3", "Explore", nil)

	var count int32
	exec := &ResearchExecutor{
		RunTask: func(ctx context.Context, task *ResearchTask) (string, error) {
			atomic.AddInt32(&count, 1)
			return "result-" + task.ID, nil
		},
		MaxParallel: 4,
	}

	results, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results["t1"] != "result-t1" {
		t.Fatalf("t1 result = %q", results["t1"])
	}
	if int(atomic.LoadInt32(&count)) != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestExecuteLinearChain(t *testing.T) {
	plan := NewResearchPlanV2("test")
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.AddTask("t2", "q2", "Plan", []string{"t1"})
	plan.AddTask("t3", "q3", "Explore", []string{"t2"})

	var order []string
	exec := &ResearchExecutor{
		RunTask: func(ctx context.Context, task *ResearchTask) (string, error) {
			order = append(order, task.ID)
			return "r-" + task.ID, nil
		},
		MaxParallel: 4,
	}

	results, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	// With a linear chain (serial by dependency), order must be sequential.
	if len(order) != 3 || order[0] != "t1" || order[1] != "t2" || order[2] != "t3" {
		t.Fatalf("order = %v, want [t1, t2, t3]", order)
	}
}

func TestExecuteDiamondDependency(t *testing.T) {
	plan := NewResearchPlanV2("test")
	plan.AddTask("A", "qA", "Explore", nil)
	plan.AddTask("B", "qB", "Plan", []string{"A"})
	plan.AddTask("C", "qC", "Explore", []string{"A"})
	plan.AddTask("D", "qD", "Plan", []string{"B", "C"})

	var mu sync.Mutex
	completed := make(map[string]bool)
	exec := &ResearchExecutor{
		RunTask: func(ctx context.Context, task *ResearchTask) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			// When D runs, B and C must already be done.
			if task.ID == "D" {
				if !completed["B"] || !completed["C"] {
					return "", fmt.Errorf("D ran before B and C completed")
				}
			}
			// When B or C runs, A must be done.
			if task.ID == "B" || task.ID == "C" {
				if !completed["A"] {
					return "", fmt.Errorf("%s ran before A completed", task.ID)
				}
			}
			completed[task.ID] = true
			return "r-" + task.ID, nil
		},
		MaxParallel: 4,
	}

	results, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
}

func TestExecuteDeadlockDetection(t *testing.T) {
	plan := NewResearchPlanV2("test")
	// Circular dependency: t1 -> t2 -> t1.
	plan.AddTask("t1", "q1", "Explore", []string{"t2"})
	plan.AddTask("t2", "q2", "Plan", []string{"t1"})

	exec := &ResearchExecutor{
		RunTask: func(ctx context.Context, task *ResearchTask) (string, error) {
			return "done", nil
		},
		MaxParallel: 4,
	}

	_, err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected deadlock error")
	}
}

func TestExecuteNilRunTask(t *testing.T) {
	plan := NewResearchPlanV2("test")
	exec := &ResearchExecutor{}
	_, err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for nil RunTask")
	}
}

func TestExecuteTaskFailure(t *testing.T) {
	plan := NewResearchPlanV2("test")
	plan.AddTask("t1", "q1", "Explore", nil)

	exec := &ResearchExecutor{
		RunTask: func(ctx context.Context, task *ResearchTask) (string, error) {
			return "", fmt.Errorf("simulated failure")
		},
		MaxParallel: 4,
	}

	_, err := exec.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error from task failure")
	}
}

func TestSynthesizeWithPrompt(t *testing.T) {
	plan := NewResearchPlanV2("q")
	plan.Synthesizer = "Combine these:"
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.AddTask("t2", "q2", "Plan", nil)

	results := map[string]string{
		"t1": "Finding 1",
		"t2": "Finding 2",
	}

	output := Synthesize(plan, results)
	if output == "" {
		t.Fatal("Synthesize returned empty")
	}
	if !contains(output, "Combine these:") {
		t.Fatal("output should contain synthesizer prompt")
	}
	if !contains(output, "Finding 1") || !contains(output, "Finding 2") {
		t.Fatal("output should contain both results")
	}
}

func TestSynthesizeEmpty(t *testing.T) {
	plan := NewResearchPlanV2("q")
	output := Synthesize(plan, map[string]string{})
	if output != "" {
		t.Fatalf("expected empty, got %q", output)
	}
}

func TestSynthesizeNoPrompt(t *testing.T) {
	plan := NewResearchPlanV2("q")
	plan.AddTask("t1", "q1", "Explore", nil)
	results := map[string]string{"t1": "result"}

	output := Synthesize(plan, results)
	if contains(output, plan.Synthesizer) && plan.Synthesizer != "" {
		t.Fatal("should not contain empty synthesizer")
	}
	if !contains(output, "result") {
		t.Fatal("output should contain the result")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
