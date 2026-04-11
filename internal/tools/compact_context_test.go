package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestCompactContextToolSpec(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	spec, ok := r.Get("compact_context")
	if !ok {
		t.Fatal("compact_context tool should be registered")
	}
	if spec.RequiredMode != permission.ReadOnly {
		t.Errorf("expected ReadOnly permission, got %v", spec.RequiredMode)
	}
	if spec.Source != SourceBuiltin {
		t.Errorf("expected SourceBuiltin, got %v", spec.Source)
	}
	if spec.AlwaysAvailable {
		t.Error("compact_context should be a deferred tool")
	}
}

func TestRegisterCompactContextHandler(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	called := false
	RegisterCompactContextHandler(r, func(ctx context.Context) (string, int, int, error) {
		called = true
		return "test summary of compacted content", 10, 4, nil
	})

	spec, _ := r.Get("compact_context")
	if spec.Handler == nil {
		t.Fatal("handler should be set after registration")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{"reason": "topic shift"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("compact function should have been called")
	}
	if !strings.Contains(result, "Compacted: 10 messages") {
		t.Errorf("result should contain compacted count, got: %s", result)
	}
	if !strings.Contains(result, "Preserved: 4 messages") {
		t.Errorf("result should contain preserved count, got: %s", result)
	}
	if !strings.Contains(result, "topic shift") {
		t.Errorf("result should contain the reason, got: %s", result)
	}
}

func TestRegisterCompactContextHandler_NoInput(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	RegisterCompactContextHandler(r, func(ctx context.Context) (string, int, int, error) {
		return "summary", 5, 3, nil
	})

	spec, _ := r.Get("compact_context")
	result, err := spec.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "successfully") {
		t.Errorf("should report success, got: %s", result)
	}
}

func TestRegisterCompactContextHandler_Error(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	RegisterCompactContextHandler(r, func(ctx context.Context) (string, int, int, error) {
		return "", 0, 0, fmt.Errorf("too few messages to compact")
	})

	spec, _ := r.Get("compact_context")
	_, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error when compaction fails")
	}
	if !strings.Contains(err.Error(), "too few messages") {
		t.Errorf("error should propagate reason, got: %v", err)
	}
}

func TestTruncateStr(t *testing.T) {
	short := "hello"
	if got := truncateStr(short, 10); got != short {
		t.Errorf("short string should not be truncated, got %q", got)
	}

	long := strings.Repeat("a", 100)
	if got := truncateStr(long, 10); len(got) != 13 { // 10 + "..."
		t.Errorf("long string should be truncated to 13 chars, got %d", len(got))
	}
}
