package conversation

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/tools"
)

func TestStreamingExecutor_ConcurrentSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	reg := tools.NewRegistry()

	var callCount atomic.Int32

	_ = reg.Register(&tools.ToolSpec{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			callCount.Add(1)
			return "file content", nil
		},
		IsConcurrencySafe: true,
		RequiredMode:      permission.ReadOnly,
	})

	exec := NewStreamingToolExecutor(reg, 4)

	// Submit 3 concurrent-safe tools.
	for i := 0; i < 3; i++ {
		exec.Submit(context.Background(), i, ToolCall{
			ID:    "tc" + string(rune('0'+i)),
			Name:  "read_file",
			Input: json.RawMessage(`{}`),
		})
	}

	results := exec.Wait(context.Background())
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if callCount.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", callCount.Load())
	}

	for i := 0; i < 3; i++ {
		r, ok := results[i]
		if !ok {
			t.Fatalf("missing result for index %d", i)
		}
		if r.err != nil {
			t.Fatalf("result %d error: %v", i, r.err)
		}
		if r.output != "file content" {
			t.Fatalf("result %d: expected 'file content', got %q", i, r.output)
		}
	}
}

func TestStreamingExecutor_DeferredNonConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	reg := tools.NewRegistry()

	_ = reg.Register(&tools.ToolSpec{
		Name:        "edit_file",
		Description: "Edit a file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "edited", nil
		},
		IsConcurrencySafe: false, // not safe for concurrent
		RequiredMode:      permission.ReadOnly,
	})

	exec := NewStreamingToolExecutor(reg, 4)
	exec.Submit(context.Background(), 0, ToolCall{
		ID:    "tc0",
		Name:  "edit_file",
		Input: json.RawMessage(`{}`),
	})

	// Deferred tools execute during Wait().
	results := exec.Wait(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].output != "edited" {
		t.Fatalf("expected 'edited', got %q", results[0].output)
	}
}

func TestStreamingExecutor_MixedConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	reg := tools.NewRegistry()

	_ = reg.Register(&tools.ToolSpec{
		Name:        "grep_search",
		Description: "Search",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "found", nil
		},
		IsConcurrencySafe: true,
		RequiredMode:      permission.ReadOnly,
	})

	_ = reg.Register(&tools.ToolSpec{
		Name:        "write_file",
		Description: "Write",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "written", nil
		},
		IsConcurrencySafe: false,
		RequiredMode:      permission.ReadOnly,
	})

	exec := NewStreamingToolExecutor(reg, 4)

	// Submit mixed tools.
	exec.Submit(context.Background(), 0, ToolCall{ID: "t0", Name: "grep_search", Input: json.RawMessage(`{}`)})
	exec.Submit(context.Background(), 1, ToolCall{ID: "t1", Name: "write_file", Input: json.RawMessage(`{}`)})
	exec.Submit(context.Background(), 2, ToolCall{ID: "t2", Name: "grep_search", Input: json.RawMessage(`{}`)})

	results := exec.Wait(context.Background())
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].output != "found" {
		t.Errorf("result 0: expected 'found', got %q", results[0].output)
	}
	if results[1].output != "written" {
		t.Errorf("result 1: expected 'written', got %q", results[1].output)
	}
	if results[2].output != "found" {
		t.Errorf("result 2: expected 'found', got %q", results[2].output)
	}
}

func TestStreamingExecutor_FormatResults(t *testing.T) {
	reg := tools.NewRegistry()
	exec := NewStreamingToolExecutor(reg, 4)

	// Manually set results.
	exec.results[0] = &streamResult{output: "ok", err: nil}
	exec.results[1] = &streamResult{output: "", err: context.Canceled}

	calls := []ToolCall{
		{ID: "t0", Name: "read"},
		{ID: "t1", Name: "bash"},
	}

	blocks := exec.FormatResults(calls)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].IsError {
		t.Error("first block should not be error")
	}
	if !blocks[1].IsError {
		t.Error("second block should be error")
	}
}
