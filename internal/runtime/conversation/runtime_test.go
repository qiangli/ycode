package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/tools"
)

// newTestRuntime creates a minimal Runtime for testing ExecuteTools.
func newTestRuntime(parallel bool, registry *tools.Registry) *Runtime {
	cfg := config.DefaultConfig()
	cfg.Parallel.Enabled = parallel
	return &Runtime{
		config:   cfg,
		registry: registry,
		logger:   slog.Default(),
	}
}

// registerTestTool adds a simple tool to the registry.
func registerTestTool(t *testing.T, reg *tools.Registry, name string, category tools.ToolCategory, handler tools.ToolFunc) {
	t.Helper()
	err := reg.Register(&tools.ToolSpec{
		Name:     name,
		Handler:  handler,
		Category: category,
	})
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func TestExecuteTools_ParallelEnabled(t *testing.T) {
	reg := tools.NewRegistry()

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	handler := func(ctx context.Context, input json.RawMessage) (string, error) {
		cur := concurrent.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		concurrent.Add(-1)
		return "ok", nil
	}

	for i := range 4 {
		name := "tool_" + string(rune('a'+i))
		registerTestTool(t, reg, name, tools.CategoryStandard, handler)
	}

	rt := newTestRuntime(true, reg)

	calls := []ToolCall{
		{ID: "t1", Name: "tool_a", Input: json.RawMessage(`{}`)},
		{ID: "t2", Name: "tool_b", Input: json.RawMessage(`{}`)},
		{ID: "t3", Name: "tool_c", Input: json.RawMessage(`{}`)},
		{ID: "t4", Name: "tool_d", Input: json.RawMessage(`{}`)},
	}

	results := rt.ExecuteTools(context.Background(), calls, nil)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Type != api.ContentTypeToolResult {
			t.Errorf("result[%d].Type = %s, want tool_result", i, r.Type)
		}
		if r.Content != "ok" {
			t.Errorf("result[%d].Content = %q, want %q", i, r.Content, "ok")
		}
	}

	// With parallel enabled and 4 tools, concurrency should exceed 1.
	if mc := maxConcurrent.Load(); mc <= 1 {
		t.Errorf("max concurrent = %d, expected > 1 (tools should run in parallel)", mc)
	}
}

func TestExecuteTools_SequentialWhenDisabled(t *testing.T) {
	reg := tools.NewRegistry()

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	handler := func(ctx context.Context, input json.RawMessage) (string, error) {
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
	}

	registerTestTool(t, reg, "tool_x", tools.CategoryStandard, handler)
	registerTestTool(t, reg, "tool_y", tools.CategoryStandard, handler)
	registerTestTool(t, reg, "tool_z", tools.CategoryStandard, handler)

	rt := newTestRuntime(false, reg)

	calls := []ToolCall{
		{ID: "t1", Name: "tool_x", Input: json.RawMessage(`{}`)},
		{ID: "t2", Name: "tool_y", Input: json.RawMessage(`{}`)},
		{ID: "t3", Name: "tool_z", Input: json.RawMessage(`{}`)},
	}

	results := rt.ExecuteTools(context.Background(), calls, nil)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Sequential: max concurrent should be exactly 1.
	if mc := maxConcurrent.Load(); mc != 1 {
		t.Errorf("max concurrent = %d, expected 1 (sequential execution)", mc)
	}
}

func TestExecuteTools_SingleCallAlwaysSequential(t *testing.T) {
	reg := tools.NewRegistry()
	registerTestTool(t, reg, "solo", tools.CategoryStandard, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "done", nil
	})

	rt := newTestRuntime(true, reg)

	calls := []ToolCall{
		{ID: "t1", Name: "solo", Input: json.RawMessage(`{}`)},
	}

	results := rt.ExecuteTools(context.Background(), calls, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "done" {
		t.Errorf("content = %q, want %q", results[0].Content, "done")
	}
}

func TestExecuteTools_PreservesToolUseID(t *testing.T) {
	reg := tools.NewRegistry()
	registerTestTool(t, reg, "echo", tools.CategoryStandard, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "ok", nil
	})

	rt := newTestRuntime(true, reg)

	calls := []ToolCall{
		{ID: "toolu_abc123", Name: "echo", Input: json.RawMessage(`{}`)},
		{ID: "toolu_def456", Name: "echo", Input: json.RawMessage(`{}`)},
	}

	results := rt.ExecuteTools(context.Background(), calls, nil)

	if results[0].ToolUseID != "toolu_abc123" {
		t.Errorf("result[0].ToolUseID = %q, want %q", results[0].ToolUseID, "toolu_abc123")
	}
	if results[1].ToolUseID != "toolu_def456" {
		t.Errorf("result[1].ToolUseID = %q, want %q", results[1].ToolUseID, "toolu_def456")
	}
}

func TestExecuteTools_ErrorMarkedAsIsError(t *testing.T) {
	reg := tools.NewRegistry()
	registerTestTool(t, reg, "fail", tools.CategoryStandard, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "", fmt.Errorf("something broke")
	})

	rt := newTestRuntime(true, reg)

	calls := []ToolCall{
		{ID: "t1", Name: "fail", Input: json.RawMessage(`{}`)},
	}

	results := rt.ExecuteTools(context.Background(), calls, nil)

	if !results[0].IsError {
		t.Error("expected IsError=true for failed tool")
	}
	if results[0].Content != "Error: something broke" {
		t.Errorf("error content = %q", results[0].Content)
	}
}

func TestExecuteTools_ParallelProgressEvents(t *testing.T) {
	reg := tools.NewRegistry()
	registerTestTool(t, reg, "fast_a", tools.CategoryStandard, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "a", nil
	})
	registerTestTool(t, reg, "fast_b", tools.CategoryStandard, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "b", nil
	})

	rt := newTestRuntime(true, reg)

	calls := []ToolCall{
		{ID: "t1", Name: "fast_a", Input: json.RawMessage(`{}`)},
		{ID: "t2", Name: "fast_b", Input: json.RawMessage(`{}`)},
	}

	progress := make(chan taskqueue.TaskEvent, 20)
	rt.ExecuteTools(context.Background(), calls, progress)
	close(progress)

	var events []taskqueue.TaskEvent
	for ev := range progress {
		events = append(events, ev)
	}

	// Each tool should produce at least 2 events (running + completed/failed).
	if len(events) < 4 {
		t.Errorf("expected at least 4 progress events, got %d", len(events))
	}

	var hasCompleted bool
	for _, ev := range events {
		if ev.Status == taskqueue.StatusCompleted {
			hasCompleted = true
		}
	}
	if !hasCompleted {
		t.Error("expected at least one StatusCompleted event")
	}
}

func TestSanitizeUserMessageForFlush(t *testing.T) {
	t.Run("removes tool_result blocks and keeps text", func(t *testing.T) {
		msg := api.Message{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_123",
					Content:   "file contents",
				},
				{
					Type: api.ContentTypeText,
					Text: "Now fix the bug",
				},
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_456",
					Content:   "other result",
				},
			},
		}

		result := sanitizeUserMessageForFlush(msg)

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 block, got %d", len(result.Content))
		}
		if result.Content[0].Type != api.ContentTypeText {
			t.Errorf("expected text block, got %s", result.Content[0].Type)
		}
		if result.Content[0].Text != "Now fix the bug" {
			t.Errorf("expected text preserved, got %q", result.Content[0].Text)
		}
		if result.Role != api.RoleUser {
			t.Errorf("expected user role, got %s", result.Role)
		}
	})

	t.Run("fallback when all blocks are tool_results", func(t *testing.T) {
		msg := api.Message{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_123",
					Content:   "result 1",
				},
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_456",
					Content:   "result 2",
				},
			},
		}

		result := sanitizeUserMessageForFlush(msg)

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 fallback block, got %d", len(result.Content))
		}
		if result.Content[0].Type != api.ContentTypeText {
			t.Errorf("expected text block, got %s", result.Content[0].Type)
		}
		if result.Content[0].Text != "Please continue from where we left off." {
			t.Errorf("unexpected fallback text: %q", result.Content[0].Text)
		}
	})

	t.Run("preserves non-tool-result blocks", func(t *testing.T) {
		msg := api.Message{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{
					Type: api.ContentTypeText,
					Text: "hello",
				},
				{
					Type:  api.ContentTypeToolUse,
					ID:    "t1",
					Name:  "bash",
					Input: json.RawMessage(`{"cmd":"ls"}`),
				},
			},
		}

		result := sanitizeUserMessageForFlush(msg)

		if len(result.Content) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(result.Content))
		}
		if result.Content[0].Text != "hello" {
			t.Errorf("expected text preserved, got %q", result.Content[0].Text)
		}
		if result.Content[1].ID != "t1" {
			t.Errorf("expected tool use ID preserved, got %q", result.Content[1].ID)
		}
	})
}
