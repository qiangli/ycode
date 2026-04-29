package toolexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExecutor_HostExec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := New(nil, nil) // no container engine, no gap recorder
	e.Register(&ToolDef{
		Name:   "echo",
		Binary: "echo",
	})

	result, err := e.Run(context.Background(), "echo", "", "hello", "world")
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != TierHostExec {
		t.Errorf("expected TierHostExec, got %v", result.Tier)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecutor_NativeTier(t *testing.T) {
	called := false
	e := New(nil, nil)
	e.Register(&ToolDef{
		Name:   "test-tool",
		Binary: "nonexistent-binary-xyz",
		NativeFuncs: map[string]NativeFunc{
			"hello": func(ctx context.Context, dir string, args []string) (*Result, error) {
				called = true
				return &Result{Stdout: "native output"}, nil
			},
		},
	})

	result, err := e.Run(context.Background(), "test-tool", "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("native func was not called")
	}
	if result.Tier != TierNative {
		t.Errorf("expected TierNative, got %v", result.Tier)
	}
	if result.Stdout != "native output" {
		t.Errorf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecutor_NativeFallthrough(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	e := New(nil, nil)
	e.Register(&ToolDef{
		Name:   "echo",
		Binary: "echo",
		NativeFuncs: map[string]NativeFunc{
			"hello": func(ctx context.Context, dir string, args []string) (*Result, error) {
				return nil, ErrNotImplemented
			},
		},
	})

	// Should fall through from native to host exec.
	result, err := e.Run(context.Background(), "echo", "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != TierHostExec {
		t.Errorf("expected TierHostExec after fallthrough, got %v", result.Tier)
	}
}

func TestExecutor_UnknownTool(t *testing.T) {
	e := New(nil, nil)
	_, err := e.Run(context.Background(), "nonexistent", "", "arg")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestExecutor_NoHostBinaryNoEngine(t *testing.T) {
	e := New(nil, nil)
	e.Register(&ToolDef{
		Name:   "fake",
		Binary: "nonexistent-binary-xyz-12345",
	})

	_, err := e.Run(context.Background(), "fake", "", "arg")
	if err == nil {
		t.Error("expected error when no host binary and no engine")
	}
}

func TestExecutor_GapRecorder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	recorded := false
	var recordedTier Tier
	recorder := &mockRecorder{fn: func(ctx context.Context, category, subcommand string, tier Tier) {
		recorded = true
		recordedTier = tier
	}}

	e := New(nil, recorder)
	e.Register(&ToolDef{
		Name:   "echo",
		Binary: "echo",
	})

	_, err := e.Run(context.Background(), "echo", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !recorded {
		t.Error("gap was not recorded")
	}
	if recordedTier != TierHostExec {
		t.Errorf("expected TierHostExec, got %v", recordedTier)
	}
}

func TestExecutor_GitDef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Skip if git is not available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}

	// Create a temp git repo.
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	e := New(nil, nil)
	e.Register(NewGitDef())

	result, err := e.Run(context.Background(), "git", dir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout == "" {
		t.Error("expected git log output")
	}
	if result.Tier != TierHostExec {
		t.Errorf("expected TierHostExec, got %v", result.Tier)
	}
}

func TestTier_String(t *testing.T) {
	tests := []struct {
		tier Tier
		want string
	}{
		{TierNative, "native"},
		{TierHostExec, "host-exec"},
		{TierContainer, "container"},
		{Tier(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("Tier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

type mockRecorder struct {
	fn func(ctx context.Context, category, subcommand string, tier Tier)
}

func (m *mockRecorder) RecordGap(ctx context.Context, category, subcommand string, tier Tier) {
	m.fn(ctx, category, subcommand, tier)
}
