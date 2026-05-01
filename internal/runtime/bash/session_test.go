package bash

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestShellSession_WorkDir(t *testing.T) {
	s := NewShellSession("/tmp")
	if got := s.WorkDir(); got != "/tmp" {
		t.Errorf("expected /tmp, got %q", got)
	}
}

func TestShellSession_SetWorkDir(t *testing.T) {
	s := NewShellSession("/initial")
	s.SetWorkDir("/new/dir")
	if got := s.WorkDir(); got != "/new/dir" {
		t.Errorf("expected '/new/dir', got %q", got)
	}
}

func TestShellSession_WrapCommand_PassThrough(t *testing.T) {
	s := NewShellSession("/initial")
	cmd := "echo hello"
	if got := s.WrapCommand(cmd); got != cmd {
		t.Errorf("WrapCommand should return command as-is, got %q", got)
	}
}

func TestShellSession_ParseOutput_PassThrough(t *testing.T) {
	s := NewShellSession("/initial")
	stdout := "just some output\n"
	if got := s.ParseOutput(stdout); got != stdout {
		t.Errorf("ParseOutput should return stdout as-is, got %q", got)
	}
}

func TestShellSession_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	s := NewShellSession(tmpDir)
	executor := NewInterpreterExecutor(s, permission.DangerFullAccess)

	// First command: cd into subdir.
	result1, err := executor.Execute(context.Background(), ExecParams{
		Command: "cd sub && echo in_sub",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result1.Stdout != "in_sub" {
		t.Errorf("expected 'in_sub', got %q", result1.Stdout)
	}
	if got := s.WorkDir(); got != subDir {
		t.Errorf("expected workDir %q, got %q", subDir, got)
	}

	// Second command: should run in the tracked workDir.
	result2, err := executor.Execute(context.Background(), ExecParams{
		Command: "pwd",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result2.Stdout != subDir {
		t.Errorf("expected pwd=%q, got %q", subDir, result2.Stdout)
	}
}
