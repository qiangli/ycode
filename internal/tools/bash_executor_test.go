package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/computer"
	"github.com/qiangli/ycode/internal/runtime/permission"
)

// mockExecutor records calls for testing.
type mockExecutor struct {
	lastParams bash.ExecParams
	result     *bash.ExecResult
	err        error
}

func (m *mockExecutor) Execute(ctx context.Context, params bash.ExecParams) (*bash.ExecResult, error) {
	m.lastParams = params
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// mockShell adapts a mockExecutor to computer.Shell for tests.
type mockShell struct{ exec *mockExecutor }

func (s *mockShell) Run(ctx context.Context, p bash.ExecParams) (*bash.ExecResult, error) {
	return s.exec.Execute(ctx, p)
}

func (s *mockShell) Session(context.Context, computer.SessionOpts) (computer.Session, error) {
	return nil, computer.ErrNotSupported
}

// hostShell is a Shell that runs commands directly via bash.Execute.
// Used by tests that exercise the real host-execution path without
// constructing a full Computer.
type hostShell struct{}

func (hostShell) Run(ctx context.Context, p bash.ExecParams) (*bash.ExecResult, error) {
	return bash.Execute(ctx, p)
}

func (hostShell) Session(context.Context, computer.SessionOpts) (computer.Session, error) {
	return nil, computer.ErrNotSupported
}

func TestRegisterBashHandler_WithExecutor(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:            "bash",
		Description:     "test bash",
		InputSchema:     json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		RequiredMode:    permission.DangerFullAccess,
		AlwaysAvailable: true,
	})

	// Set permission resolver to allow all.
	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	mock := &mockExecutor{
		result: &bash.ExecResult{
			Stdout:   "container output",
			ExitCode: 0,
		},
	}

	RegisterBashHandler(reg, "/workspace", nil, &mockShell{exec: mock})

	spec, ok := reg.Get("bash")
	if !ok {
		t.Fatal("bash tool not found")
	}

	input := json.RawMessage(`{"command": "echo hello"}`)
	output, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "container output" {
		t.Errorf("expected 'container output', got %q", output)
	}

	// Command is wrapped with session's cwd tracking.
	if !strings.Contains(mock.lastParams.Command, "echo hello") {
		t.Errorf("expected command to contain 'echo hello', got %q", mock.lastParams.Command)
	}
}

func TestRegisterBashHandler_WithoutExecutor(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:            "bash",
		Description:     "test bash",
		InputSchema:     json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		RequiredMode:    permission.DangerFullAccess,
		AlwaysAvailable: true,
	})

	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	// Register without executor — should use host execution.
	RegisterBashHandler(reg, t.TempDir(), nil, hostShell{})

	spec, ok := reg.Get("bash")
	if !ok {
		t.Fatal("bash tool not found")
	}

	input := json.RawMessage(`{"command": "echo hello_from_host"}`)
	output, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "hello_from_host" {
		t.Errorf("expected 'hello_from_host', got %q", output)
	}
}

func TestRegisterBashHandler_WorkDirDefault(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:         "bash",
		Description:  "test bash",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		RequiredMode: permission.DangerFullAccess,
	})

	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	mock := &mockExecutor{
		result: &bash.ExecResult{Stdout: "ok", ExitCode: 0},
	}

	workDir := "/test/workspace"
	RegisterBashHandler(reg, workDir, nil, &mockShell{exec: mock})

	spec, _ := reg.Get("bash")
	input := json.RawMessage(`{"command": "pwd"}`)
	spec.Handler(context.Background(), input)

	// Should have defaulted to workDir since no WorkDir in params.
	if mock.lastParams.WorkDir != workDir {
		t.Errorf("expected workDir %q, got %q", workDir, mock.lastParams.WorkDir)
	}
}

func TestRegisterBashHandler_ExitCode(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:         "bash",
		Description:  "test bash",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		RequiredMode: permission.DangerFullAccess,
	})

	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	mock := &mockExecutor{
		result: &bash.ExecResult{
			Stdout:   "some output",
			Stderr:   "some error",
			ExitCode: 1,
		},
	}

	RegisterBashHandler(reg, "/workspace", nil, &mockShell{exec: mock})

	spec, _ := reg.Get("bash")
	input := json.RawMessage(`{"command": "false"}`)
	output, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Output should include stdout, stderr, and exit code.
	if output == "" {
		t.Error("expected non-empty output")
	}
}
