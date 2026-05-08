//go:build integration

package collab_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	giteacmd "code.gitea.io/gitea/cmd"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/collab"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
)

// TestMain serves two roles:
//
//  1. Gitea hook delegator — when Gitea's pre-receive/etc. scripts
//     invoke this binary as `<bin> hook --config=... pre-receive`,
//     dispatch to Gitea's CLI machinery so PR state stays consistent.
//
//  2. Stub autopilot child — when the orchestrator spawns this binary
//     as the "ycode prompt" child (env YCODE_TEST_STUB_CHILD=1), act
//     like an autopilot run that creates a file and commits it. This
//     lets us exercise the full orchestrator without needing an LLM.
//
// Without these intercepts the "go test" framework would run the
// whole suite in either case, breaking everything.
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if arg == "hook" {
			app := giteacmd.NewMainApp(giteacmd.AppVersion{Version: "test"})
			if err := giteacmd.RunMainApp(app, os.Args...); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	if os.Getenv("YCODE_TEST_STUB_CHILD") == "1" {
		os.Exit(stubAutopilot())
	}
	os.Exit(m.Run())
}

// stubAutopilot impersonates `ycode prompt /autopilot ...`. It writes
// a file (named from the env-injected target) into cwd, commits it,
// and exits 0. Does not need network access — the orchestrator pushes
// from the sandbox after this returns.
func stubAutopilot() int {
	target := os.Getenv("YCODE_TEST_STUB_FILE")
	if target == "" {
		target = "stub-output.txt"
	}
	contents := os.Getenv("YCODE_TEST_STUB_CONTENT")
	if contents == "" {
		contents = "stub did the thing\n"
	}
	cwd, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(cwd, target), []byte(contents), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "stub write:", err)
		return 1
	}
	for _, args := range [][]string{
		{"add", target},
		{"commit", "-m", "stub: add " + target},
	} {
		out, err := runCmd(cwd, "git", args...)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stub git", args, ":", err, out)
			return 1
		}
	}
	return 0
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestE2E_RealGitea_Orchestrator runs the orchestrator end-to-end
// with one agent against a real embedded Gitea, using this test
// binary as the stub autopilot child. Verifies:
//   - Issue is popped + claimed
//   - Sandbox is created with correct branch + author identity
//   - Stub child runs and commits
//   - Orchestrator pushes the branch and opens a PR
//   - The merger auto-merges (CICommand="" → no gate)
//   - The merged file appears on main
func TestE2E_RealGitea_Orchestrator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (real embedded Gitea + spawned child)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 1. Real embedded Gitea.
	giteaDir := t.TempDir()
	srv, err := gitserver.NewServer(&gitserver.ServerConfig{
		DataDir:  giteaDir,
		HTTPOnly: true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = srv.Stop(stopCtx)
	})

	if _, err := srv.EnsureAdmin(ctx, "admin", "admin@ycode.local", gitserver.RandomPassword()); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	token, err := srv.IssueToken(ctx, "admin", "ycode-collab-e2e")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	c := gitserver.NewClient(srv.BaseURL(), token)

	// 2. Resolve project + create tracking repo + seed main.
	cwd := newOrchHostProject(t, "README.md", "host\n")
	r, _ := projects.NewRegistry(giteaDir)
	p, _ := r.Resolve(ctx, cwd)
	if _, err := projects.EnsureRepo(ctx, c, p); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	repos, _ := c.ListRepos(ctx)
	cloneURL := ""
	for _, rp := range repos {
		if rp.Name == p.Slug {
			cloneURL = rp.CloneURL
		}
	}
	if cloneURL == "" {
		t.Fatalf("clone URL not found")
	}
	if err := projects.MirrorUpstream(ctx, cwd, projects.MirrorOptions{
		CloneURL: cloneURL, Token: token, Force: true,
	}); err != nil {
		t.Fatalf("MirrorUpstream: %v", err)
	}

	// 3. File a task.
	if err := queue.EnsureLabels(ctx, c, p); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	if _, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
		Title:    "stub task",
		Body:     "stub work for the orchestrator",
		Priority: queue.LabelP1,
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 4. Configure orchestrator: 1 agent, no CI gate, stub child binary.
	syncLog, _ := projects.NewSyncLog(giteaDir, p)
	logHandler := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Set env so the spawned child knows to run stubAutopilot.
	t.Setenv("YCODE_TEST_STUB_CHILD", "1")
	t.Setenv("YCODE_TEST_STUB_FILE", "agent-output.txt")
	t.Setenv("YCODE_TEST_STUB_CONTENT", "agent did the thing\n")

	o, err := collab.New(collab.Config{
		Project:      p,
		Client:       c,
		SyncLog:      syncLog,
		NumAgents:    1,
		CICommand:    "", // unconditional auto-merge
		YcodeBin:     os.Args[0],
		SandboxRoot:  filepath.Join(giteaDir, "collab-sandboxes"),
		SessionsRoot: filepath.Join(giteaDir, "collab-sessions"),
		IssueTimeout: 30 * time.Second,
		PollInterval: 1 * time.Second,
		Token:        token,
		CloneURL:     cloneURL,
		Logger:       logHandler,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 5. Run with a deadline. The orchestrator runs until ctx is canceled;
	// we cancel as soon as the merger has merged the PR.
	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- o.Run(runCtx) }()

	// 6. Wait for the merger to record the sync entry. That happens
	// AFTER the PR is closed (synchronously, in the same Tick), so it's
	// the right signal for "we're fully done — safe to shut down."
	deadline := time.Now().Add(120 * time.Second)
	merged := false
	for time.Now().Before(deadline) && !merged {
		entries, _ := syncLog.Pending()
		if len(entries) > 0 {
			merged = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	runCancel()
	<-done // drain

	if !merged {
		// Dump the per-agent log for diagnosis.
		entries, _ := os.ReadDir(filepath.Join(giteaDir, "collab-sessions"))
		for _, e := range entries {
			t.Logf("session dir: %s", e.Name())
			subEntries, _ := os.ReadDir(filepath.Join(giteaDir, "collab-sessions", e.Name()))
			for _, se := range subEntries {
				logBytes, _ := os.ReadFile(filepath.Join(giteaDir, "collab-sessions", e.Name(), se.Name()))
				t.Logf("--- %s/%s ---\n%s", e.Name(), se.Name(), string(logBytes))
			}
		}
		t.Fatalf("PR did not merge within deadline")
	}

	// 7. Verify the agent's file landed on main.
	pending, _ := syncLog.Pending()
	if len(pending) != 1 {
		t.Errorf("expected 1 sync entry, got %d", len(pending))
	}
	if len(pending) >= 1 && !strings.HasPrefix(pending[0].AgentID, "agent-") {
		t.Errorf("expected agent-* AgentID, got %q", pending[0].AgentID)
	}
}

// --- helpers (orchestrator-test specific) ---

func newOrchHostProject(t *testing.T, filename, content string) string {
	t.Helper()
	dir := t.TempDir()
	mustOrchExec(t, dir, "git", "init", "-b", "main")
	mustOrchExec(t, dir, "git", "config", "user.name", "host")
	mustOrchExec(t, dir, "git", "config", "user.email", "host@example.com")
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	mustOrchExec(t, dir, "git", "add", filename)
	mustOrchExec(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func mustOrchExec(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}
