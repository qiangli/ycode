package selfinit

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// stubTool is a tiny Tool impl for orchestrator tests.
type stubTool struct {
	mu               sync.Mutex
	name             string
	detected         bool
	mcpCalls         int
	instructionCalls int
	mcpErr           error
	instructionErr   error
}

func (s *stubTool) Name() string { return s.name }
func (s *stubTool) Detect() bool { return s.detected }

func (s *stubTool) WriteMCP(_ context.Context, _ []CapabilitySpec) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mcpCalls++
	if s.mcpErr != nil {
		return false, s.mcpErr
	}
	return true, nil
}

func (s *stubTool) WriteInstructions(_ context.Context, _ []CapabilitySpec) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instructionCalls++
	if s.instructionErr != nil {
		return false, s.instructionErr
	}
	return true, nil
}

func TestRun_FullFlow(t *testing.T) {
	repo := makeRepo(t)
	home := t.TempDir()
	stub := &stubTool{name: "stub", detected: true}

	res, err := Run(context.Background(), Options{
		Cwd:          repo,
		Home:         home,
		YcodeVersion: "test",
		Tools:        []Tool{stub},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RepoRoot != repo {
		t.Errorf("RepoRoot=%q want %q", res.RepoRoot, repo)
	}
	if res.Skipped {
		t.Errorf("first run should not be skipped")
	}
	if len(res.Capabilities) == 0 {
		t.Errorf("expected baseline caps registered")
	}
	if stub.mcpCalls != 1 || stub.instructionCalls != 1 {
		t.Errorf("stub not invoked: mcp=%d instr=%d", stub.mcpCalls, stub.instructionCalls)
	}
	if _, ok := res.UserFilesByTool["stub"]; !ok {
		t.Errorf("stub not in UserFilesByTool: %+v", res.UserFilesByTool)
	}

	// Second run with same state must be skipped.
	res2, err := Run(context.Background(), Options{
		Cwd:          repo,
		Home:         home,
		YcodeVersion: "test",
		Tools:        []Tool{stub},
	})
	if err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if !res2.Skipped {
		t.Errorf("second run should skip via marker")
	}
	if stub.mcpCalls != 1 {
		t.Errorf("stub re-invoked despite skip: mcpCalls=%d", stub.mcpCalls)
	}

	// Force=true bypasses the marker.
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "test", Force: true,
		Tools: []Tool{stub},
	}); err != nil {
		t.Fatalf("Run --force: %v", err)
	}
	if stub.mcpCalls != 2 {
		t.Errorf("force did not re-invoke: mcpCalls=%d", stub.mcpCalls)
	}
}

func TestRun_OptedOut(t *testing.T) {
	repo := makeRepo(t)
	if err := WriteOptOut(repo); err != nil {
		t.Fatal(err)
	}
	stub := &stubTool{name: "stub", detected: true}
	res, err := Run(context.Background(), Options{
		Cwd: repo, Home: t.TempDir(), Tools: []Tool{stub},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.OptedOut {
		t.Errorf("expected OptedOut=true")
	}
	if stub.mcpCalls != 0 {
		t.Errorf("stub invoked despite opt-out")
	}
}

func TestRun_NotInGitRepo_StillRunsUserScope(t *testing.T) {
	cwd := t.TempDir() // no .git
	stub := &stubTool{name: "stub", detected: true}
	res, err := Run(context.Background(), Options{
		Cwd: cwd, Home: t.TempDir(), Tools: []Tool{stub},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RepoRoot != "" {
		t.Errorf("expected empty RepoRoot, got %q", res.RepoRoot)
	}
	if stub.mcpCalls != 1 || stub.instructionCalls != 1 {
		t.Errorf("user-scope writes should still happen outside git repo")
	}
	// No marker file written outside git repo.
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "ycode", ".init-done")); err == nil {
		t.Errorf(".agents/ycode/.init-done should not be created outside a git repo")
	}
}

func TestRun_UndetectedToolSkipped(t *testing.T) {
	repo := makeRepo(t)
	notInstalled := &stubTool{name: "missing", detected: false}
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: t.TempDir(), Tools: []Tool{notInstalled},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if notInstalled.mcpCalls != 0 {
		t.Errorf("undetected tool should not be invoked")
	}
}

// TestRun_ForeignToolsOptIn verifies that auto-discovery of foreign-tool
// writers from the package registry is gated by RegisterForeignTools
// (or YCODE_SELFINIT_FOREIGN=1). Explicit Tools lists are unaffected.
func TestRun_ForeignToolsOptIn(t *testing.T) {
	saved := toolRegistry
	t.Cleanup(func() { toolRegistry = saved })
	toolRegistry = nil

	registered := &stubTool{name: "registered", detected: true}
	RegisterTool(registered)

	repo := makeRepo(t)
	home := t.TempDir()

	// Default: no opt-in → registered tool not invoked.
	t.Setenv("YCODE_SELFINIT_FOREIGN", "")
	if _, err := Run(context.Background(), Options{Cwd: repo, Home: home, YcodeVersion: "v1"}); err != nil {
		t.Fatalf("Run default: %v", err)
	}
	if registered.mcpCalls != 0 {
		t.Errorf("registered tool invoked without opt-in: mcpCalls=%d", registered.mcpCalls)
	}

	// Opt-in via Options field.
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "v2", Force: true,
		RegisterForeignTools: true,
	}); err != nil {
		t.Fatalf("Run opt-in: %v", err)
	}
	if registered.mcpCalls != 1 {
		t.Errorf("opt-in via Options did not invoke registered tool: mcpCalls=%d", registered.mcpCalls)
	}

	// Opt-in via env var.
	t.Setenv("YCODE_SELFINIT_FOREIGN", "1")
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "v3", Force: true,
	}); err != nil {
		t.Fatalf("Run env opt-in: %v", err)
	}
	if registered.mcpCalls != 2 {
		t.Errorf("env opt-in did not invoke registered tool: mcpCalls=%d", registered.mcpCalls)
	}
}

func TestRegisterTool_Idempotent(t *testing.T) {
	// Save and restore the registry — this test mutates package state.
	saved := toolRegistry
	t.Cleanup(func() { toolRegistry = saved })
	toolRegistry = nil

	a := &stubTool{name: "x"}
	b := &stubTool{name: "x"} // same name
	c := &stubTool{name: "y"}
	RegisterTool(a)
	RegisterTool(b) // should be no-op
	RegisterTool(c)
	if got := len(registeredTools()); got != 2 {
		t.Errorf("expected 2 tools, got %d", got)
	}
}
