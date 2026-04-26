package bash

import (
	"context"
	"testing"
)

func TestHostExecutor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	exec := &HostExecutor{}
	result, err := exec.Execute(context.Background(), ExecParams{
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("HostExecutor.Execute: %v", err)
	}
	if result.Stdout != "hello" {
		t.Errorf("expected 'hello', got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestContainerExecutorNilContainer(t *testing.T) {
	exec := &ContainerExecutor{Container: nil}
	_, err := exec.Execute(context.Background(), ExecParams{
		Command: "echo hello",
	})
	if err == nil {
		t.Error("expected error for nil container")
	}
}

func TestExecuteWithNilExecutor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// nil executor should fall back to host execution.
	result, err := ExecuteWith(context.Background(), nil, ExecParams{
		Command: "echo fallback",
	})
	if err != nil {
		t.Fatalf("ExecuteWith nil: %v", err)
	}
	if result.Stdout != "fallback" {
		t.Errorf("expected 'fallback', got %q", result.Stdout)
	}
}
