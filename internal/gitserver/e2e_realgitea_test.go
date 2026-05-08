//go:build integration

package gitserver_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitsvchttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/agents"
	"github.com/qiangli/ycode/internal/gitserver/merger"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
)

// TestE2E_RealGitea is the umbrella for every real-embedded-Gitea
// integration test in this package. They all share a single Gitea
// instance because Gitea's package-global state (setting.*, route
// registry) cannot survive two NewServer cycles in one process.
//
// Subtests:
//   - bootstrap_idempotency: EnsureAdmin + IssueToken behavior
//   - full_workflow:         end-to-end multi-agent flow over real HTTP
//
// Run with: make test-gitserver
func TestE2E_RealGitea(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (real embedded Gitea)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
		t.Fatalf("Server.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = srv.Stop(stopCtx)
	})

	if _, err := srv.EnsureAdmin(ctx, "admin", "admin@ycode.local", gitserver.RandomPassword()); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	token, err := srv.IssueToken(ctx, "admin", "ycode-e2e")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	c := gitserver.NewClient(srv.BaseURL(), token)
	testToken = token // exposed for subtest helpers (runMergerOnce)

	t.Run("bootstrap_idempotency", func(t *testing.T) {
		// EnsureAdmin must be a no-op on repeat (returns same user).
		u1, err := srv.EnsureAdmin(ctx, "admin", "admin@ycode.local", gitserver.RandomPassword())
		if err != nil {
			t.Fatalf("EnsureAdmin: %v", err)
		}
		u2, err := srv.EnsureAdmin(ctx, "admin", "admin@ycode.local", gitserver.RandomPassword())
		if err != nil {
			t.Fatalf("EnsureAdmin (idempotent): %v", err)
		}
		if u1.ID != u2.ID {
			t.Errorf("idempotent EnsureAdmin: ID drift %d → %d", u1.ID, u2.ID)
		}

		// Second IssueToken with same name must succeed via the unique-suffix path.
		tok2, err := srv.IssueToken(ctx, "admin", "ycode-e2e")
		if err != nil {
			t.Fatalf("IssueToken (second): %v", err)
		}
		if tok2 == token {
			t.Error("expected distinct token on second IssueToken")
		}
		c2 := gitserver.NewClient(srv.BaseURL(), tok2)
		if _, err := c2.ListRepos(ctx); err != nil {
			t.Fatalf("ListRepos with second token: %v", err)
		}
	})

	t.Run("full_workflow", func(t *testing.T) {
		runFullWorkflow(ctx, t, c, token, giteaDir)
	})

	t.Run("ci_gate_green", func(t *testing.T) {
		f := prepareCollabPR(t, ctx, c, token, giteaDir, "ci-green.txt", "ok\n")
		runMergerOnce(t, ctx, c, f, "true")
		assertPRClosed(t, ctx, c, f)
		assertSyncEntry(t, f, 1)
	})

	t.Run("ci_gate_red", func(t *testing.T) {
		f := prepareCollabPR(t, ctx, c, token, giteaDir, "ci-red.txt", "fail\n")
		runMergerOnce(t, ctx, c, f, "false")
		// Red CI: the PR must remain open and no sync entry recorded.
		all, err := c.ListPRs(ctx, projects.Owner, f.project.Slug, "all")
		if err != nil {
			t.Fatalf("ListPRs: %v", err)
		}
		for _, pr := range all {
			if pr.Number == f.pr.Number && pr.State != "open" {
				t.Errorf("expected PR open after red CI, got state=%q", pr.State)
			}
		}
		entries, _ := f.syncLog.Pending()
		if len(entries) != 0 {
			t.Errorf("expected 0 sync entries on red CI, got %d", len(entries))
		}
	})

	t.Run("pull_clean", func(t *testing.T) {
		f := prepareCollabPR(t, ctx, c, token, giteaDir, "pull-clean.txt", "ok\n")
		runMergerOnce(t, ctx, c, f, "")
		assertPRClosed(t, ctx, c, f)
		// Now pull. cwd has nothing, upstream has the merged file.
		if err := projects.PullFastForward(ctx, f.cwd, f.cloneURL, token); err != nil {
			t.Fatalf("PullFastForward (clean): %v", err)
		}
		if _, err := os.Stat(filepath.Join(f.cwd, "pull-clean.txt")); err != nil {
			t.Fatalf("expected pull-clean.txt in cwd after pull: %v", err)
		}
	})

	t.Run("pull_dirty", func(t *testing.T) {
		f := prepareCollabPR(t, ctx, c, token, giteaDir, "pull-dirty.txt", "ok\n")
		runMergerOnce(t, ctx, c, f, "")
		assertPRClosed(t, ctx, c, f)
		// Make cwd dirty.
		if err := os.WriteFile(filepath.Join(f.cwd, "uncommitted.txt"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatalf("write dirty file: %v", err)
		}
		err := projects.PullFastForward(ctx, f.cwd, f.cloneURL, token)
		if err == nil {
			t.Fatal("expected ErrPullDirty, got nil")
		}
		if !errors.Is(err, projects.ErrPullDirty) {
			t.Errorf("expected ErrPullDirty, got: %v", err)
		}
		// And the merged file should NOT have been pulled.
		if _, err := os.Stat(filepath.Join(f.cwd, "pull-dirty.txt")); !os.IsNotExist(err) {
			t.Errorf("expected pull-dirty.txt absent on refused pull, stat err: %v", err)
		}
	})

	t.Run("pull_non_ff", func(t *testing.T) {
		f := prepareCollabPR(t, ctx, c, token, giteaDir, "pull-divergent.txt", "ok\n")
		runMergerOnce(t, ctx, c, f, "")
		assertPRClosed(t, ctx, c, f)
		// Make cwd diverge: add a different file and commit.
		if err := os.WriteFile(filepath.Join(f.cwd, "local-only.txt"), []byte("local\n"), 0o644); err != nil {
			t.Fatalf("write local file: %v", err)
		}
		mustExecCwd(t, f.cwd, "git", "add", "local-only.txt")
		mustExecCwd(t, f.cwd, "git", "commit", "-m", "local divergent commit")
		err := projects.PullFastForward(ctx, f.cwd, f.cloneURL, token)
		if err == nil {
			t.Fatal("expected ErrPullNotFastForward, got nil")
		}
		if !errors.Is(err, projects.ErrPullNotFastForward) {
			t.Errorf("expected ErrPullNotFastForward, got: %v", err)
		}
	})
}

// runFullWorkflow exercises the full multi-agent collab workflow.
// Lives in a helper so it can be inlined as a subtest of TestE2E_RealGitea.
// giteaDir must be the same data dir the parent Gitea instance uses, so
// the SyncLog/merger-work paths line up.
func runFullWorkflow(ctx context.Context, t *testing.T, c *gitserver.Client, token, giteaDir string) {
	// 3. Build a host project: a real local git repo with one commit.
	cwd := newRealHostProject(t, "hello.txt", "hi from cwd\n")

	// 4. Resolve project + ensure tracking repo.
	r, err := projects.NewRegistry(giteaDir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	p, err := r.Resolve(ctx, cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := projects.EnsureRepo(ctx, c, p); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	// Read clone URL from Gitea.
	repos, err := c.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	cloneURL := ""
	for _, rp := range repos {
		if rp.Name == p.Slug {
			cloneURL = rp.CloneURL
		}
	}
	if cloneURL == "" {
		t.Fatalf("clone URL not found for slug %s", p.Slug)
	}

	// 5. Mirror cwd → admin/<slug>:main via REAL `git push` over HTTP.
	if err := projects.MirrorUpstream(ctx, cwd, projects.MirrorOptions{
		CloneURL: cloneURL,
		Token:    token,
		Force:    true,
	}); err != nil {
		t.Fatalf("MirrorUpstream: %v", err)
	}

	// 6. EnsureLabels + Submit.
	if err := queue.EnsureLabels(ctx, c, p); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	issue, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
		Title:    "Add greeting.txt",
		Body:     "Create greeting.txt with 'hello world'",
		Priority: queue.LabelP1,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 7. Pop as agent-A.
	popped, err := queue.Pop(ctx, c, p, "agent-A")
	if err != nil || popped == nil {
		t.Fatalf("Pop: %v %v", err, popped)
	}
	if popped.Number != issue.Number {
		t.Fatalf("Pop wrong issue: %d", popped.Number)
	}

	// 8. AssignBranch.
	a := &agents.Agent{ID: "agent-A", Name: "alice"}
	br, err := agents.AssignBranch(ctx, c, p, a, popped.Number)
	if err != nil {
		t.Fatalf("AssignBranch: %v", err)
	}

	// 9. Simulate the agent's work: clone, switch to branch, edit, commit.
	agentWork := simulateAgentEdits(t, ctx, cloneURL, token, br.Name, "greeting.txt", "hello world\n", a)

	// 10. Push the agent's branch to Gitea.
	if err := br.Push(ctx, agents.PushOptions{
		WorktreePath: agentWork,
		CloneURL:     cloneURL,
		Token:        token,
	}); err != nil {
		t.Fatalf("Branch.Push: %v", err)
	}

	// 11. OpenPR.
	pr, err := br.OpenPR(ctx, c, "", "")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}

	// 12. Run merger — no CI command, auto-merge unconditionally.
	syncLog, err := projects.NewSyncLog(giteaDir, p)
	if err != nil {
		t.Fatalf("NewSyncLog: %v", err)
	}
	m, err := merger.New(merger.Config{
		Client:    c,
		Project:   p,
		SyncLog:   syncLog,
		CloneURL:  cloneURL,
		Token:     token,
		WorkDir:   filepath.Join(giteaDir, "merger-work"),
		CICommand: "",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("merger.New: %v", err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("merger.Tick: %v", err)
	}

	// 13. Assertions.
	allPRs, err := c.ListPRs(ctx, projects.Owner, p.Slug, "all")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	var mergedPR *gitserver.PullRequest
	for i := range allPRs {
		if allPRs[i].Number == pr.Number {
			mergedPR = &allPRs[i]
		}
	}
	if mergedPR == nil {
		t.Fatalf("PR #%d not found", pr.Number)
	}
	if mergedPR.State != "closed" {
		t.Errorf("PR state: got %q want closed", mergedPR.State)
	}

	// 14. Verify the merge actually landed on main: clone fresh and stat the file.
	verify := t.TempDir()
	if _, err := git.PlainCloneContext(ctx, verify, false, &git.CloneOptions{
		URL:           cloneURL,
		Auth:          &gitsvchttp.BasicAuth{Username: "token", Password: token},
		ReferenceName: plumbing.NewBranchReferenceName("main"),
		SingleBranch:  true,
	}); err != nil {
		t.Fatalf("clone main for verification: %v", err)
	}
	if _, err := os.Stat(filepath.Join(verify, "greeting.txt")); err != nil {
		t.Fatalf("greeting.txt not on main after merge: %v", err)
	}

	// 15. Sync log should have one entry, attributed to agent-A.
	pending, err := syncLog.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 sync entry, got %d", len(pending))
	}
	if pending[0].AgentID != "agent-A" {
		t.Errorf("synclog AgentID: got %q want agent-A", pending[0].AgentID)
	}
	if pending[0].PR != pr.Number {
		t.Errorf("synclog PR: got %d want %d", pending[0].PR, pr.Number)
	}
}

// --- helpers ---

func newRealHostProject(t *testing.T, filename, content string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	hr := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := repo.Storer.SetReference(hr); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "host", Email: "host@example.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

func simulateAgentEdits(t *testing.T, ctx context.Context, cloneURL, token, branch, filename, content string, a *agents.Agent) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           cloneURL,
		Auth:          &gitsvchttp.BasicAuth{Username: "token", Password: token},
		ReferenceName: plumbing.NewBranchReferenceName("main"),
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("clone for agent work: %v", err)
	}
	wt, _ := repo.Worktree()
	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
	}); err != nil {
		t.Fatalf("checkout %s: %v", branch, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("add: %v", err)
	}
	parts := strings.SplitN(a.AuthorTrailer(), " <", 2)
	authorName := parts[0]
	authorEmail := strings.TrimSuffix(parts[1], ">")
	if _, err := wt.Commit("agent: add "+filename, &git.CommitOptions{
		Author: &object.Signature{Name: authorName, Email: authorEmail, When: time.Now()},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

// --- subtest fixture & helpers ---

// collabFixture captures the live state needed by the CI-gate and pull
// subtests. Each subtest gets its own fresh project/cwd/syncLog so they
// don't depend on each other's state.
type collabFixture struct {
	cwd      string
	project  *projects.Project
	cloneURL string
	syncLog  *projects.SyncLog
	pr       *gitserver.PullRequest
	branch   *agents.Branch
}

// prepareCollabPR builds an end-to-end collab state up to "PR open":
// fresh host project, mirrored to upstream, one issue submitted +
// claimed, agent branch with the named file pushed, PR opened.
// Caller invokes the merger as needed.
func prepareCollabPR(t *testing.T, ctx context.Context, c *gitserver.Client, token, giteaDir, fileName, content string) *collabFixture {
	t.Helper()
	cwd := newRealHostProject(t, "host.txt", "host\n")
	r, err := projects.NewRegistry(giteaDir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	p, err := r.Resolve(ctx, cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
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
		t.Fatalf("clone URL not found for slug %s", p.Slug)
	}

	if err := projects.MirrorUpstream(ctx, cwd, projects.MirrorOptions{
		CloneURL: cloneURL, Token: token, Force: true,
	}); err != nil {
		t.Fatalf("MirrorUpstream: %v", err)
	}
	if err := queue.EnsureLabels(ctx, c, p); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	issue, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
		Title:    "subtest: " + fileName,
		Priority: queue.LabelP1,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	popped, err := queue.Pop(ctx, c, p, "agent-S")
	if err != nil || popped == nil || popped.Number != issue.Number {
		t.Fatalf("Pop: %v %v", err, popped)
	}
	a := &agents.Agent{ID: "agent-S"}
	br, err := agents.AssignBranch(ctx, c, p, a, popped.Number)
	if err != nil {
		t.Fatalf("AssignBranch: %v", err)
	}
	work := simulateAgentEdits(t, ctx, cloneURL, token, br.Name, fileName, content, a)
	if err := br.Push(ctx, agents.PushOptions{
		WorktreePath: work,
		CloneURL:     cloneURL,
		Token:        token,
	}); err != nil {
		t.Fatalf("Branch.Push: %v", err)
	}
	pr, err := br.OpenPR(ctx, c, "", "")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}

	syncLog, err := projects.NewSyncLog(giteaDir, p)
	if err != nil {
		t.Fatalf("NewSyncLog: %v", err)
	}

	return &collabFixture{
		cwd:      cwd,
		project:  p,
		cloneURL: cloneURL,
		syncLog:  syncLog,
		pr:       pr,
		branch:   br,
	}
}

// runMergerOnce constructs a merger with the given CICommand and runs
// one Tick. CICommand="" disables the gate (auto-merge).
func runMergerOnce(t *testing.T, ctx context.Context, c *gitserver.Client, f *collabFixture, ciCommand string) {
	t.Helper()
	m, err := merger.New(merger.Config{
		Client:    c,
		Project:   f.project,
		SyncLog:   f.syncLog,
		CloneURL:  f.cloneURL,
		Token:     testToken,
		WorkDir:   t.TempDir(),
		CICommand: ciCommand,
		Logger:    slog.New(slog.NewTextHandler(testLogWriter{t: t}, nil)),
	})
	if err != nil {
		t.Fatalf("merger.New: %v", err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("merger.Tick: %v", err)
	}
}

// testLogWriter routes slog output to t.Log so the merger's warnings
// show up in the failing test's output.
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// testToken holds the bootstrap token for the lifetime of TestE2E_RealGitea.
// Set by TestE2E_RealGitea before the subtests run.
var testToken string

func assertPRClosed(t *testing.T, ctx context.Context, c *gitserver.Client, f *collabFixture) {
	t.Helper()
	all, err := c.ListPRs(ctx, projects.Owner, f.project.Slug, "all")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	for _, pr := range all {
		if pr.Number == f.pr.Number {
			if pr.State != "closed" {
				t.Errorf("PR #%d state: got %q want closed", pr.Number, pr.State)
			}
			return
		}
	}
	t.Fatalf("PR #%d not found", f.pr.Number)
}

func assertSyncEntry(t *testing.T, f *collabFixture, want int) {
	t.Helper()
	pending, err := f.syncLog.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != want {
		t.Errorf("sync entries: got %d want %d", len(pending), want)
	}
}

func mustExecCwd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}
