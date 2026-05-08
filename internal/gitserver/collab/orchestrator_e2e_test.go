//go:build integration

package collab_test

import (
	"context"
	"fmt"
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
// a file into cwd, commits it, and exits 0.
//
// Filename is derived from the current git branch (e.g.
// agent/agent-x/issue-3 → "stub-issue-3.txt"). This lets multiple
// concurrent agents in the same project each commit their own file
// without conflicting at merge time.
func stubAutopilot() int {
	cwd, _ := os.Getwd()
	branchOut, err := runCmd(cwd, "git", "branch", "--show-current")
	if err != nil {
		fmt.Fprintln(os.Stderr, "stub: read branch:", err, branchOut)
		return 1
	}
	branch := strings.TrimSpace(branchOut)
	target := stubFileFromBranch(branch)
	contents := fmt.Sprintf("stub did %q on %s\n", target, branch)
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

// stubFileFromBranch maps "agent/<id>/issue-N" → "stub-issue-N.txt".
// Falls back to the branch name as-is for free-form / unparseable
// branches.
func stubFileFromBranch(branch string) string {
	const marker = "/issue-"
	if i := strings.LastIndex(branch, marker); i >= 0 {
		return "stub-issue-" + branch[i+len(marker):] + ".txt"
	}
	return "stub-" + sanitizeBranch(branch) + ".txt"
}

func sanitizeBranch(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestE2E_RealGitea_Orchestrator runs the orchestrator end-to-end
// against a real embedded Gitea, using this test binary as the stub
// autopilot child.
//
// Subtests share one Gitea instance (Gitea's package-global state
// can't survive two NewServer cycles in one process):
//   - single_issue: 1 agent, 1 issue → merges
//   - multi_issue:  2 agents, 3 issues → all 3 merge
func TestE2E_RealGitea_Orchestrator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (real embedded Gitea + spawned child)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()

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

	t.Run("single_issue", func(t *testing.T) {
		runOrchestratorSubtest(ctx, t, c, token, giteaDir, 1, 1)
	})

	t.Run("multi_issue", func(t *testing.T) {
		runOrchestratorSubtest(ctx, t, c, token, giteaDir, 2, 3)
	})
}

// runOrchestratorSubtest sets up a fresh project, files numIssues tasks,
// runs the orchestrator with numAgents agents, and asserts that all
// numIssues PRs merge within the wall-clock budget.
func runOrchestratorSubtest(ctx context.Context, t *testing.T, c *gitserver.Client, token, giteaDir string, numAgents, numIssues int) {
	t.Helper()

	// Each subtest gets its own project (fresh slug) so PR/issue numbers
	// don't collide across subtests.
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
		t.Fatalf("clone URL not found for %s", p.Slug)
	}
	if err := projects.MirrorUpstream(ctx, cwd, projects.MirrorOptions{
		CloneURL: cloneURL, Token: token, Force: true,
	}); err != nil {
		t.Fatalf("MirrorUpstream: %v", err)
	}
	if err := queue.EnsureLabels(ctx, c, p); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	for i := 1; i <= numIssues; i++ {
		if _, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
			Title:    fmt.Sprintf("stub task %d", i),
			Body:     fmt.Sprintf("issue %d body", i),
			Priority: queue.LabelP1,
		}); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	syncLog, _ := projects.NewSyncLog(giteaDir, p)
	logHandler := slog.New(slog.NewTextHandler(testLogWriterCollab{t: t}, nil))

	// Tell the spawned child binary to behave as stubAutopilot.
	t.Setenv("YCODE_TEST_STUB_CHILD", "1")

	o, err := collab.New(collab.Config{
		Project:      p,
		Client:       c,
		SyncLog:      syncLog,
		NumAgents:    numAgents,
		CICommand:    "", // unconditional auto-merge for the test
		YcodeBin:     os.Args[0],
		SandboxRoot:  filepath.Join(giteaDir, "collab-sandboxes-"+p.Slug),
		SessionsRoot: filepath.Join(giteaDir, "collab-sessions-"+p.Slug),
		IssueTimeout: 30 * time.Second,
		PollInterval: 1 * time.Second,
		Token:        token,
		CloneURL:     cloneURL,
		Logger:       logHandler,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- o.Run(runCtx) }()

	// Wait for all numIssues sync entries.
	deadline := time.Now().Add(time.Duration(numIssues) * 60 * time.Second)
	got := 0
	for time.Now().Before(deadline) {
		entries, _ := syncLog.Pending()
		if len(entries) >= numIssues {
			got = len(entries)
			break
		}
		time.Sleep(1 * time.Second)
	}
	runCancel()
	<-done

	if got < numIssues {
		// Dump per-agent logs for diagnosis.
		entries, _ := os.ReadDir(filepath.Join(giteaDir, "collab-sessions-"+p.Slug))
		for _, e := range entries {
			subEntries, _ := os.ReadDir(filepath.Join(giteaDir, "collab-sessions-"+p.Slug, e.Name()))
			for _, se := range subEntries {
				logBytes, _ := os.ReadFile(filepath.Join(giteaDir, "collab-sessions-"+p.Slug, e.Name(), se.Name()))
				t.Logf("--- %s/%s ---\n%s", e.Name(), se.Name(), string(logBytes))
			}
		}
		t.Fatalf("expected %d sync entries within deadline, got %d", numIssues, got)
	}

	// Each entry must be attributed to an agent.
	pending, _ := syncLog.Pending()
	if len(pending) != numIssues {
		t.Errorf("sync entries: got %d want %d", len(pending), numIssues)
	}
	for i, e := range pending {
		if !strings.HasPrefix(e.AgentID, "agent-") {
			t.Errorf("entry %d: AgentID %q lacks agent- prefix", i, e.AgentID)
		}
	}

	// All issues must be closed in Gitea.
	closedIssues, _ := c.ListIssues(ctx, projects.Owner, p.Slug, "closed", nil)
	if len(closedIssues) != numIssues {
		t.Errorf("closed issues: got %d want %d", len(closedIssues), numIssues)
	}

	// And the agent-derived files must be on main: clone fresh, stat each.
	verify := t.TempDir()
	if _, err := gitClone(ctx, verify, cloneURL, token); err != nil {
		t.Fatalf("verify clone: %v", err)
	}
	for i := 1; i <= numIssues; i++ {
		want := fmt.Sprintf("stub-issue-%d.txt", i)
		if _, err := os.Stat(filepath.Join(verify, want)); err != nil {
			t.Errorf("expected %s on main: %v", want, err)
		}
	}
}

// testLogWriterCollab routes slog output to t.Log.
type testLogWriterCollab struct{ t *testing.T }

func (w testLogWriterCollab) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// gitClone shells out for a fresh, single-branch clone of admin/<slug>:main.
func gitClone(ctx context.Context, dir, cloneURL, token string) (string, error) {
	authURL := injectTokenForTest(cloneURL, token)
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", "main", "--single-branch", authURL, dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func injectTokenForTest(rawURL, token string) string {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	scheme := "http://"
	rest := strings.TrimPrefix(rawURL, scheme)
	if rest == rawURL {
		scheme = "https://"
		rest = strings.TrimPrefix(rawURL, scheme)
	}
	return fmt.Sprintf("%stoken:%s@%s", scheme, token, rest)
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
