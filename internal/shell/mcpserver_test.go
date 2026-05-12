package shell

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// callMCP exercises the MCPHandler end-to-end and parses the returned
// envelope so tests can assert on stdout/stderr/exit_code together.
func callMCP(t *testing.T, h *MCPHandler, args map[string]any) Envelope {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := h.HandleToolCall(context.Background(), "agent_shell", raw)
	if err != nil {
		t.Fatalf("HandleToolCall: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, out)
	}
	return env
}

// TestMCPHandler_DefaultCwdInheritsRuntime confirms stdio behavior: when
// no cwd is passed, the call runs in the runtime's WorkDir.
func TestMCPHandler_DefaultCwdInheritsRuntime(t *testing.T) {
	rtDir := t.TempDir()
	rt := newTestRuntime(t, Options{WorkDir: rtDir})
	h := NewMCPHandler(rt)

	env := callMCP(t, h, map[string]any{
		"command": "pwd",
	})
	if env.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", env.ExitCode, env.Stderr)
	}
	if got := strings.TrimSpace(env.Stdout); !samePath(got, rtDir) {
		t.Errorf("stdout = %q, want %q", got, rtDir)
	}
}

// TestMCPHandler_PerCallCwdOverridesRuntime confirms HTTP behavior: a
// per-call cwd reroutes the dispatch into a different tree without
// touching the shared runtime.
func TestMCPHandler_PerCallCwdOverridesRuntime(t *testing.T) {
	rtDir := t.TempDir()
	callDir := t.TempDir()
	rt := newTestRuntime(t, Options{WorkDir: rtDir})
	h := NewMCPHandler(rt)

	env := callMCP(t, h, map[string]any{
		"command": "pwd",
		"cwd":     callDir,
	})
	if env.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", env.ExitCode, env.Stderr)
	}
	if got := strings.TrimSpace(env.Stdout); !samePath(got, callDir) {
		t.Errorf("stdout = %q, want %q", got, callDir)
	}
	// Shared runtime's session must still report the original WorkDir —
	// no mutation by the per-call dispatch.
	if got := rt.WorkDir(); got != rtDir {
		t.Errorf("runtime WorkDir mutated: got %q, want %q", got, rtDir)
	}
}

// TestMCPHandler_RejectsRelativeCwd: structured error in stderr,
// non-zero exit, no command actually run.
func TestMCPHandler_RejectsRelativeCwd(t *testing.T) {
	rt := newTestRuntime(t, Options{})
	h := NewMCPHandler(rt)

	env := callMCP(t, h, map[string]any{
		"command": "pwd",
		"cwd":     "relative/path",
	})
	if env.ExitCode == 0 {
		t.Errorf("expected non-zero exit for relative cwd, got 0; stdout=%q", env.Stdout)
	}
	if !strings.Contains(env.Stderr, "absolute path") {
		t.Errorf("stderr = %q, want containing %q", env.Stderr, "absolute path")
	}
}

// TestMCPHandler_RejectsNonexistentCwd: stat error surfaces as stderr.
func TestMCPHandler_RejectsNonexistentCwd(t *testing.T) {
	rt := newTestRuntime(t, Options{})
	h := NewMCPHandler(rt)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	env := callMCP(t, h, map[string]any{
		"command": "pwd",
		"cwd":     missing,
	})
	if env.ExitCode == 0 {
		t.Errorf("expected non-zero exit for missing cwd, got 0; stdout=%q", env.Stdout)
	}
	if !strings.Contains(env.Stderr, missing) {
		t.Errorf("stderr should mention missing path, got %q", env.Stderr)
	}
}

// samePath compares two paths for equality after resolving any symlinks.
// On darwin t.TempDir() returns /var/folders/... but pwd in a subshell
// can echo back either /var/... or /private/var/... depending on how the
// path was passed in — symlink-resolution-aware compare avoids the flake.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra != "" && ra == rb
}
