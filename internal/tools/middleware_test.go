package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWithRetry_SucceedsFirstAttempt(t *testing.T) {
	calls := 0
	handler := func(_ context.Context, _ json.RawMessage) (string, error) {
		calls++
		return "ok", nil
	}

	mw := WithRetry(3, 10*time.Millisecond)
	wrapped := mw(handler)
	result, err := wrapped(context.Background(), nil)

	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestWithRetry_SucceedsAfterFailures(t *testing.T) {
	calls := 0
	handler := func(_ context.Context, _ json.RawMessage) (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("transient")
		}
		return "recovered", nil
	}

	mw := WithRetry(3, 10*time.Millisecond)
	wrapped := mw(handler)
	result, err := wrapped(context.Background(), nil)

	if err != nil {
		t.Fatal(err)
	}
	if result != "recovered" {
		t.Errorf("expected 'recovered', got %q", result)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestWithRetry_ExhaustsAttempts(t *testing.T) {
	calls := 0
	handler := func(_ context.Context, _ json.RawMessage) (string, error) {
		calls++
		return "", errors.New("permanent")
	}

	mw := WithRetry(2, 10*time.Millisecond)
	wrapped := mw(handler)
	_, err := wrapped(context.Background(), nil)

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestWithTimeout_Respected(t *testing.T) {
	handler := func(ctx context.Context, _ json.RawMessage) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			return "too late", nil
		}
	}

	mw := WithTimeout(50 * time.Millisecond)
	wrapped := mw(handler)
	_, err := wrapped(context.Background(), nil)

	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWithTimeout_CompletesInTime(t *testing.T) {
	handler := func(_ context.Context, _ json.RawMessage) (string, error) {
		return "fast", nil
	}

	mw := WithTimeout(1 * time.Second)
	wrapped := mw(handler)
	result, err := wrapped(context.Background(), nil)

	if err != nil {
		t.Fatal(err)
	}
	if result != "fast" {
		t.Errorf("expected 'fast', got %q", result)
	}
}

func TestGlobalMiddleware_AppliedToAllInvocations(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:            "test-tool",
		Handler:         func(_ context.Context, _ json.RawMessage) (string, error) { return "result", nil },
		AlwaysAvailable: true,
	})

	var callLog []string
	var mu sync.Mutex

	r.UseMiddleware(func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			mu.Lock()
			callLog = append(callLog, "before")
			mu.Unlock()
			res, err := next(ctx, input)
			mu.Lock()
			callLog = append(callLog, "after")
			mu.Unlock()
			return res, err
		}
	})

	result, err := r.Invoke(context.Background(), "test-tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "result" {
		t.Errorf("expected 'result', got %q", result)
	}

	if len(callLog) != 2 || callLog[0] != "before" || callLog[1] != "after" {
		t.Errorf("middleware not called correctly: %v", callLog)
	}
}

func TestGlobalMiddleware_Ordering(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:            "test-tool",
		Handler:         func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
		AlwaysAvailable: true,
	})

	var order []int
	var mu sync.Mutex

	for i := range 3 {
		idx := i
		r.UseMiddleware(func(next ToolFunc) ToolFunc {
			return func(ctx context.Context, input json.RawMessage) (string, error) {
				mu.Lock()
				order = append(order, idx)
				mu.Unlock()
				return next(ctx, input)
			}
		})
	}

	_, err := r.Invoke(context.Background(), "test-tool", nil)
	if err != nil {
		t.Fatal(err)
	}

	// First registered middleware should execute first (outermost).
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("middleware ordering wrong: %v", order)
	}
}

type testEventWriter struct {
	mu     sync.Mutex
	events []ToolEvent
}

func (w *testEventWriter) WriteEvent(e ToolEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, e)
}

func TestWithEventWriter_EmitsEvents(t *testing.T) {
	w := &testEventWriter{}
	handler := func(ctx context.Context, _ json.RawMessage) (string, error) {
		ew := ToolEventWriterFromContext(ctx)
		if ew != nil {
			ew.WriteEvent(ToolEvent{Type: ToolEventDelta, Payload: "partial"})
		}
		return "done", nil
	}

	ctx := ContextWithToolName(context.Background(), "my-tool")
	mw := WithEventWriter(w)
	wrapped := mw(handler)
	_, err := wrapped(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(w.events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(w.events), w.events)
	}
	if w.events[0].Type != ToolEventStarted {
		t.Errorf("event 0: expected started, got %s", w.events[0].Type)
	}
	if w.events[1].Type != ToolEventDelta {
		t.Errorf("event 1: expected delta, got %s", w.events[1].Type)
	}
	if w.events[2].Type != ToolEventFinished {
		t.Errorf("event 2: expected finished, got %s", w.events[2].Type)
	}
}

func TestToolEventWriterFromContext_NilWhenAbsent(t *testing.T) {
	w := ToolEventWriterFromContext(context.Background())
	if w != nil {
		t.Error("expected nil writer from empty context")
	}
}
