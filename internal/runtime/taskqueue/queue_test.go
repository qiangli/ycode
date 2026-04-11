package taskqueue

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutorOrdering(t *testing.T) {
	exec := NewExecutor(4, 2)
	calls := make([]Call, 5)
	for i := range calls {
		calls[i] = Call{
			Index:  i,
			Name:   fmt.Sprintf("tool_%d", i),
			Detail: fmt.Sprintf("tool_%d()", i),
			Invoke: func(ctx context.Context) (string, error) {
				return fmt.Sprintf("result_%d", i), nil
			},
		}
	}

	results := exec.Run(context.Background(), calls, nil)

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Index != i {
			t.Errorf("result[%d].Index = %d", i, r.Index)
		}
		expected := fmt.Sprintf("result_%d", i)
		if r.Output != expected {
			t.Errorf("result[%d].Output = %q, want %q", i, r.Output, expected)
		}
		if r.Err != nil {
			t.Errorf("result[%d].Err = %v", i, r.Err)
		}
	}
}

func TestExecutorParallelism(t *testing.T) {
	exec := NewExecutor(4, 2)

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	calls := make([]Call, 8)
	for i := range calls {
		calls[i] = Call{
			Index: i,
			Name:  fmt.Sprintf("tool_%d", i),
			Invoke: func(ctx context.Context) (string, error) {
				cur := concurrent.Add(1)
				// Track max concurrency.
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				concurrent.Add(-1)
				return "ok", nil
			},
		}
	}

	results := exec.Run(context.Background(), calls, nil)

	if len(results) != 8 {
		t.Fatalf("expected 8 results, got %d", len(results))
	}
	if mc := maxConcurrent.Load(); mc < 2 {
		t.Errorf("max concurrent = %d, expected >= 2 (tools should run in parallel)", mc)
	}
	if mc := maxConcurrent.Load(); mc > 4 {
		t.Errorf("max concurrent = %d, expected <= 4 (maxStandard limit)", mc)
	}
}

func TestExecutorLLMLimit(t *testing.T) {
	exec := NewExecutor(8, 2)

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	calls := make([]Call, 6)
	for i := range calls {
		calls[i] = Call{
			Index:    i,
			Name:     fmt.Sprintf("agent_%d", i),
			Category: CatLLM,
			Invoke: func(ctx context.Context) (string, error) {
				cur := concurrent.Add(1)
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				concurrent.Add(-1)
				return "ok", nil
			},
		}
	}

	results := exec.Run(context.Background(), calls, nil)

	if len(results) != 6 {
		t.Fatalf("expected 6 results, got %d", len(results))
	}
	if mc := maxConcurrent.Load(); mc > 2 {
		t.Errorf("max concurrent LLM = %d, expected <= 2", mc)
	}
}

func TestExecutorCancellation(t *testing.T) {
	exec := NewExecutor(2, 2)
	ctx, cancel := context.WithCancel(context.Background())

	calls := make([]Call, 4)
	for i := range calls {
		calls[i] = Call{
			Index: i,
			Name:  fmt.Sprintf("tool_%d", i),
			Invoke: func(ctx context.Context) (string, error) {
				select {
				case <-time.After(5 * time.Second):
					return "ok", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			},
		}
	}

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	results := exec.Run(ctx, calls, nil)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	// All results should have errors after cancellation.
	for i, r := range results {
		if r.Err == nil {
			t.Errorf("result[%d] expected error after cancellation", i)
		}
	}
}

func TestExecutorProgressEvents(t *testing.T) {
	exec := NewExecutor(4, 2)

	calls := []Call{
		{Index: 0, Name: "read", Detail: "Read(a.go)", Invoke: func(ctx context.Context) (string, error) {
			return "ok", nil
		}},
		{Index: 1, Name: "grep", Detail: "Grep(pattern)", Invoke: func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("not found")
		}},
	}

	progress := make(chan TaskEvent, 20)
	exec.Run(context.Background(), calls, progress)
	close(progress)

	var events []TaskEvent
	for ev := range progress {
		events = append(events, ev)
	}

	// Each call should produce: Queued, Running, Completed/Failed = 3 events each.
	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d", len(events))
	}

	// Check that we got at least one completed and one failed.
	var hasCompleted, hasFailed bool
	for _, ev := range events {
		if ev.Status == StatusCompleted {
			hasCompleted = true
		}
		if ev.Status == StatusFailed {
			hasFailed = true
		}
		if ev.Total != 2 {
			t.Errorf("event.Total = %d, want 2", ev.Total)
		}
	}
	if !hasCompleted {
		t.Error("expected at least one StatusCompleted event")
	}
	if !hasFailed {
		t.Error("expected at least one StatusFailed event")
	}
}

func TestExecutorEmptyCalls(t *testing.T) {
	exec := NewExecutor(4, 2)
	results := exec.Run(context.Background(), nil, nil)
	if results != nil {
		t.Errorf("expected nil results for empty calls, got %v", results)
	}
}

func TestExecutorSingleCall(t *testing.T) {
	exec := NewExecutor(4, 2)
	calls := []Call{{
		Index: 0,
		Name:  "read",
		Invoke: func(ctx context.Context) (string, error) {
			return "content", nil
		},
	}}

	results := exec.Run(context.Background(), calls, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Output != "content" {
		t.Errorf("got %q, want %q", results[0].Output, "content")
	}
}

func TestExecutorMixedCategories(t *testing.T) {
	exec := NewExecutor(4, 2)

	var stdConcurrent, llmConcurrent atomic.Int32
	var maxStdConcurrent, maxLLMConcurrent atomic.Int32

	trackMax := func(cur *atomic.Int32, max *atomic.Int32) {
		c := cur.Load()
		for {
			old := max.Load()
			if c <= old || max.CompareAndSwap(old, c) {
				break
			}
		}
	}

	calls := make([]Call, 8)
	// 4 standard + 4 LLM tools
	for i := range 4 {
		calls[i] = Call{
			Index:    i,
			Name:     fmt.Sprintf("std_%d", i),
			Category: CatStandard,
			Invoke: func(ctx context.Context) (string, error) {
				stdConcurrent.Add(1)
				trackMax(&stdConcurrent, &maxStdConcurrent)
				time.Sleep(20 * time.Millisecond)
				stdConcurrent.Add(-1)
				return "ok", nil
			},
		}
	}
	for j := range 4 {
		i := j + 4
		calls[i] = Call{
			Index:    i,
			Name:     fmt.Sprintf("llm_%d", i),
			Category: CatLLM,
			Invoke: func(ctx context.Context) (string, error) {
				llmConcurrent.Add(1)
				trackMax(&llmConcurrent, &maxLLMConcurrent)
				time.Sleep(20 * time.Millisecond)
				llmConcurrent.Add(-1)
				return "ok", nil
			},
		}
	}

	results := exec.Run(context.Background(), calls, nil)

	if len(results) != 8 {
		t.Fatalf("expected 8 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] unexpected error: %v", r.Index, r.Err)
		}
	}
	// Standard pool: max 4
	if mc := maxStdConcurrent.Load(); mc > 4 {
		t.Errorf("max standard concurrent = %d, expected <= 4", mc)
	}
	// LLM pool: max 2
	if mc := maxLLMConcurrent.Load(); mc > 2 {
		t.Errorf("max LLM concurrent = %d, expected <= 2", mc)
	}
}

func TestExecutorInteractiveSerialization(t *testing.T) {
	exec := NewExecutor(8, 2)

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	calls := make([]Call, 4)
	for i := range calls {
		calls[i] = Call{
			Index:    i,
			Name:     fmt.Sprintf("ask_%d", i),
			Category: CatInteractive,
			Invoke: func(ctx context.Context) (string, error) {
				cur := concurrent.Add(1)
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				concurrent.Add(-1)
				return "ok", nil
			},
		}
	}

	exec.Run(context.Background(), calls, nil)

	// Interactive tools should be serialized (max 1 at a time).
	if mc := maxConcurrent.Load(); mc > 1 {
		t.Errorf("max concurrent interactive = %d, expected 1", mc)
	}
}

func TestNewExecutorDefaults(t *testing.T) {
	exec := NewExecutor(0, -1)
	if exec.maxStandard != 8 {
		t.Errorf("maxStandard = %d, want 8", exec.maxStandard)
	}
	if exec.maxLLM != 2 {
		t.Errorf("maxLLM = %d, want 2", exec.maxLLM)
	}
}

func TestTaskStatusString(t *testing.T) {
	tests := []struct {
		s    TaskStatus
		want string
	}{
		{StatusQueued, "queued"},
		{StatusRunning, "running"},
		{StatusCompleted, "completed"},
		{StatusFailed, "failed"},
		{TaskStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("TaskStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
