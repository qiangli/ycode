package agentexec

import (
	"context"
	"testing"
	"time"
)

func TestExecutor_RunEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := NewExecutor(nil)
	preset := &AgentPreset{
		Name:       "echo",
		Command:    "echo",
		PromptFlag: "", // echo doesn't use flags
	}

	result, err := e.Run(context.Background(), preset, ExecOptions{
		ExtraArgs: []string{"hello", "world"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello world\n")
	}
	if result.TimedOut {
		t.Error("should not have timed out")
	}
	if result.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

func TestExecutor_RunTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := NewExecutor(nil)
	preset := &AgentPreset{
		Name:    "sleep",
		Command: "sleep",
	}

	result, err := e.Run(context.Background(), preset, ExecOptions{
		ExtraArgs: []string{"10"},
	}, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.TimedOut {
		t.Error("expected timeout")
	}
}

func TestExecutor_RunNonexistent(t *testing.T) {
	e := NewExecutor(nil)
	preset := &AgentPreset{
		Name:    "nonexistent",
		Command: "this-command-does-not-exist-12345",
	}

	_, err := e.Run(context.Background(), preset, ExecOptions{}, 5*time.Second)
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

func TestExecutor_RunExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := NewExecutor(nil)
	preset := &AgentPreset{
		Name:    "false",
		Command: "false",
	}

	result, err := e.Run(context.Background(), preset, ExecOptions{}, 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code from 'false' command")
	}
}

func TestExecutor_RunWithEnv(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := NewExecutor(nil)
	preset := &AgentPreset{
		Name:    "env",
		Command: "sh",
		ExtraEnv: map[string]string{
			"TEST_PRESET_VAR": "from_preset",
		},
	}

	result, err := e.Run(context.Background(), preset, ExecOptions{
		ExtraArgs: []string{"-c", "echo $TEST_PRESET_VAR $TEST_OPTS_VAR"},
		ExtraEnv: map[string]string{
			"TEST_OPTS_VAR": "from_opts",
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Stdout != "from_preset from_opts\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "from_preset from_opts\n")
	}
}

func TestExecutor_RunContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := NewExecutor(nil)
	preset := &AgentPreset{
		Name:    "sleep",
		Command: "sleep",
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := e.Run(ctx, preset, ExecOptions{
		ExtraArgs: []string{"10"},
	}, 0) // no timeout, rely on context cancellation
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Context cancellation should kill the process.
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code from cancelled process")
	}
}
