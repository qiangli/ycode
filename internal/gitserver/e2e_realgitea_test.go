//go:build integration

package gitserver_test

import (
	"context"
	"io"
	"log/slog"
	"os"
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
