//go:build e2e

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E tests for `ycode foreman` and `ycode backlog`. Drives the real
// binary; no LLM provider required (these test the control plane and
// the markdown source-of-truth, not the Worker dispatch).
//
// HOME is isolated per test so user-global skill writes don't leak.

// stateDirGlob returns the per-project state directory under HOME.
// In e2e tests the repo isn't a git remote, so the project id falls
// back to "cwd-hash:<sha8>"; rather than recomputing it the tests just
// glob the single per-project dir that ycode created.
func stateDirGlob(t *testing.T, home string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".agents", "ycode", "projects", "*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no per-project state dir under %s", home)
	}
	if len(matches) > 1 {
		t.Fatalf("multiple per-project dirs found under %s: %v", home, matches)
	}
	return matches[0]
}

// foremanStatePath returns <home>/.agents/ycode/projects/<id>/foreman/state.json.
func foremanStatePath(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(stateDirGlob(t, home), "foreman", "state.json")
}

// foremanCommandsPath returns the commands.jsonl path.
func foremanCommandsPath(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(stateDirGlob(t, home), "foreman", "commands.jsonl")
}

// backlogEntryPath returns the slug.md path inside the per-project backlog dir.
func backlogEntryPath(t *testing.T, home, slug string) string {
	t.Helper()
	return filepath.Join(stateDirGlob(t, home), "backlog", slug+".md")
}

// readForemanState parses the per-project foreman state.json.
func readForemanState(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(foremanStatePath(t, home))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse state.json: %v\ndata: %s", err, data)
	}
	return s
}

// startDaemon spawns `ycode foreman daemon` in the background. Returns
// the started cmd; caller must Kill / Wait it (test cleanup).
func startDaemon(t *testing.T, repo, home string) *exec.Cmd {
	t.Helper()
	binAbs, _ := filepath.Abs(e2eBinaryPath)
	cmd := exec.Command(binAbs, "foreman", "daemon")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TERM=dumb",
		"YCODE_NO_SERVER=1",
		"ANTHROPIC_API_KEY=", "OPENAI_API_KEY=",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// waitForState polls state.json until state == want or timeout.
func waitForState(t *testing.T, home, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(home, ".agents", "ycode", "projects", "*", "foreman", "state.json"))
		if len(matches) == 1 {
			if data, err := os.ReadFile(matches[0]); err == nil {
				var s map[string]any
				if json.Unmarshal(data, &s) == nil {
					if s["state"] == want {
						return
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("state %q not reached within %v", want, timeout)
}

func TestE2E_Foreman_DaemonStateMachine(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	repo := initRepo(t)
	home := t.TempDir()
	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	startDaemon(t, repo, home)
	waitForState(t, home, "running", 10*time.Second) // daemon writes state=running on launch

	mustForeman := func(args ...string) {
		t.Helper()
		out, err := runYcode(t, repo, home, append([]string{"foreman"}, args...)...)
		if err != nil {
			t.Fatalf("foreman %v: %v\n%s", args, err, out)
		}
	}

	mustForeman("pause")
	waitForState(t, home, "paused", 8*time.Second)

	mustForeman("resume")
	waitForState(t, home, "running", 8*time.Second)

	mustForeman("stop")
	waitForState(t, home, "stopped", 8*time.Second)

	// Sanity: state file final content reflects last_command_id (queue cursor).
	s := readForemanState(t, home)
	if s["last_command_id"] == nil || s["last_command_id"] == "" {
		t.Errorf("last_command_id not persisted: %+v", s)
	}
}

func TestE2E_Foreman_PrioWritesMarkdownFrontmatter(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	repo := initRepo(t)
	home := t.TempDir()
	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Author a backlog entry via ycode backlog new (default p2).
	if out, err := runYcode(t, repo, home, "backlog", "new", "Test task", "--slug", "demo"); err != nil {
		t.Fatalf("backlog new: %v\n%s", err, out)
	}
	mdPath := backlogEntryPath(t, home, "demo")
	if data, err := os.ReadFile(mdPath); err != nil {
		t.Fatalf("read backlog file: %v", err)
	} else if !strings.Contains(string(data), "priority: p2") {
		t.Errorf("expected priority: p2 in fresh entry, got:\n%s", data)
	}

	// Elevate via foreman prio.
	if out, err := runYcode(t, repo, home, "foreman", "prio", "demo", "p1"); err != nil {
		t.Fatalf("foreman prio: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(mdPath)
	if !strings.Contains(string(data), "priority: p1") {
		t.Errorf("priority not bumped to p1; got:\n%s", data)
	}
}

func TestE2E_Foreman_QueueIsAppendOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	repo := initRepo(t)
	home := t.TempDir()
	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	verbs := []struct {
		args []string
		verb string
	}{
		{[]string{"foreman", "start"}, "start"},
		{[]string{"foreman", "pause"}, "pause"},
		{[]string{"foreman", "resume"}, "resume"},
		{[]string{"foreman", "tell", "skip cnl, do dogfood next"}, "tell"},
		{[]string{"foreman", "skip"}, "skip"},
		{[]string{"foreman", "stop"}, "stop"},
	}
	for _, v := range verbs {
		if out, err := runYcode(t, repo, home, v.args...); err != nil {
			t.Fatalf("foreman %v: %v\n%s", v.args, err, out)
		}
	}

	data, err := os.ReadFile(foremanCommandsPath(t, home))
	if err != nil {
		t.Fatalf("read commands.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if got := len(lines); got != len(verbs) {
		t.Fatalf("expected %d lines in commands.jsonl, got %d", len(verbs), got)
	}
	for i, line := range lines {
		var c map[string]any
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Errorf("line %d not valid JSON: %v", i, err)
			continue
		}
		if c["verb"] != verbs[i].verb {
			t.Errorf("line %d verb: got %v want %s", i, c["verb"], verbs[i].verb)
		}
		if c["from"] != "cli" {
			t.Errorf("line %d: from should be 'cli', got %v", i, c["from"])
		}
		if c["id"] == nil || c["id"] == "" {
			t.Errorf("line %d: missing id", i)
		}
	}
}

func TestE2E_Foreman_StatusReportsPauseSentinel(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	repo := initRepo(t)
	home := t.TempDir()
	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Drop the PAUSE sentinel — file-based kill switch.
	pausePath := filepath.Join(repo, "docs/backlog/PAUSE")
	if err := os.WriteFile(pausePath, nil, 0o644); err != nil {
		t.Fatalf("touch PAUSE: %v", err)
	}
	out, err := runYcode(t, repo, home, "foreman", "status")
	if err != nil {
		t.Fatalf("foreman status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PAUSE sentinel:  present") {
		t.Errorf("status should report PAUSE sentinel; got:\n%s", out)
	}
}
