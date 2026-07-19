package bash

import (
	"context"
	"strings"
	"testing"
)

type mockExec struct{ n int; stderr string; code int }

func (m *mockExec) Execute(ctx context.Context, p ExecParams) (*ExecResult, error) {
	m.n++
	return &ExecResult{Stderr: m.stderr, ExitCode: m.code}, nil
}

func TestRetryTransientAppendsNote(t *testing.T) {
	t.Setenv("BASHY_HINTS", "on")
	m := &mockExec{stderr: "curl: (7) Failed to connect to x", code: 7}
	first := &ExecResult{Stderr: m.stderr, ExitCode: 7}
	out := retryTransient(context.Background(), m, ExecParams{Command: "curl x"}, first)
	if m.n < 1 {
		t.Fatalf("expected retries, executor called %d times", m.n)
	}
	if !strings.Contains(out.Stderr, "autoretry") && !strings.Contains(out.Stderr, "retried") {
		t.Fatalf("note not appended; stderr=%q (executor ran %d retries)", out.Stderr, m.n)
	}
}

func TestRetryableArgv0(t *testing.T) {
	if a, ok := retryableArgv0("curl -s http://x"); !ok || a != "curl" {
		t.Errorf("curl: got %q,%v", a, ok)
	}
	if _, ok := retryableArgv0("curl x | jq ."); ok {
		t.Error("pipeline must not be retryable")
	}
	if _, ok := retryableArgv0("curl x > out"); ok {
		t.Error("redirect must not be retryable")
	}
}
