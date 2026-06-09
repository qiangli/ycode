// Package merger watches open PRs in a project's tracking repo, runs
// the configured local CI in an isolated checkout of the prospective
// merge commit, and auto-merges on green.
//
// The merger never touches the user's working tree. On successful merge
// it appends to the project's pending-sync log; the user pulls when ready
// via `ycode tasks pull`. If the originating issue carries the push:origin
// label and an OriginPushFn is configured, the merger pushes the merged
// SHA to the host repo's "origin" remote.
//
// See docs/agent-collab.md for the full design.
package merger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
	"github.com/qiangli/ycode/internal/runtime/git"
)

// Config wires a Merger to its dependencies.
type Config struct {
	Client  *gitserver.Client
	Project *projects.Project
	SyncLog *projects.SyncLog

	// CloneURL is admin/<slug>'s http URL (Repository.CloneURL).
	CloneURL string

	// Token is the admin token, used for push/pull auth.
	Token string

	// CICommand is the shell command that defines "green CI" for this
	// project (e.g., "make build" or "go test ./..."). Empty disables CI.
	CICommand string

	// CITimeout caps how long CI may run before being killed. Zero = 30 min.
	CITimeout time.Duration

	// WorkDir is where temp checkouts live (one subdir per PR).
	// Caller passes <giteaDataDir>/merger-work; idempotent.
	WorkDir string

	// OriginPushFn, if non-nil, is invoked after a successful merge for
	// PRs whose linked issue carries the push:origin label. It receives
	// the merged SHA and is expected to push to the host repo's "origin".
	// Wired by the autopilot collab CLI; nil in tests.
	OriginPushFn func(ctx context.Context, sha string) error

	// FetchMainSHAFn, if non-nil, overrides the default git-based fetch
	// of the post-merge main SHA. Used by tests that don't have a real
	// git remote to clone from; nil in production.
	FetchMainSHAFn func(ctx context.Context, prNumber int64) (string, error)

	// AllowedCommitterEmails, when non-empty, gates auto-merge to PRs
	// whose head commit committer email matches one of the listed
	// patterns (substring match against email — e.g. "@ycode.local"
	// allows every agent identity; specific labels narrow further).
	// Empty disables the guard (v1 behavior — auto-merge anyone).
	// Defense Layer 4 from docs/loom-v2-plan.md "Defense in depth".
	AllowedCommitterEmails []string

	// Logger is required.
	Logger *slog.Logger
}

// Merger runs the auto-merge loop for one project.
type Merger struct {
	cfg Config
	ge  *git.GitExec
}

// New constructs a Merger. Validates required fields.
func New(cfg Config) (*Merger, error) {
	if cfg.Client == nil {
		return nil, errors.New("merger: nil Client")
	}
	if cfg.Project == nil {
		return nil, errors.New("merger: nil Project")
	}
	if cfg.SyncLog == nil {
		return nil, errors.New("merger: nil SyncLog")
	}
	if cfg.CloneURL == "" {
		return nil, errors.New("merger: empty CloneURL")
	}
	if cfg.Token == "" {
		return nil, errors.New("merger: empty Token")
	}
	if cfg.WorkDir == "" {
		return nil, errors.New("merger: empty WorkDir")
	}
	if cfg.CITimeout == 0 {
		cfg.CITimeout = 30 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Merger{cfg: cfg, ge: git.NewGitExec(nil)}, nil
}

// Run starts the merger loop, ticking every interval until ctx is canceled.
// Returns the context error on shutdown.
func (m *Merger) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if err := m.Tick(ctx); err != nil {
		m.cfg.Logger.Warn("merger: tick error", "err", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := m.Tick(ctx); err != nil {
				m.cfg.Logger.Warn("merger: tick error", "err", err)
			}
		}
	}
}

// TickResult is the outcome of processing one PR.
type TickResult struct {
	PRNumber int64
	Status   string // "merged" | "ci-failed" | "skipped" | "error"
	SHA      string // merged SHA (when status=merged)
	Detail   string // stderr tail or skip reason
}

// Tick processes every open PR once. Returns per-PR results for tests.
func (m *Merger) Tick(ctx context.Context) error {
	prs, err := m.cfg.Client.ListPRs(ctx, projects.Owner, m.cfg.Project.Slug, "open")
	if err != nil {
		return fmt.Errorf("merger: list PRs: %w", err)
	}
	for _, pr := range prs {
		res := m.processPR(ctx, pr)
		m.cfg.Logger.Info("merger: pr processed",
			"pr", res.PRNumber,
			"status", res.Status,
			"detail", res.Detail,
		)
	}
	return nil
}

func (m *Merger) processPR(ctx context.Context, pr gitserver.PullRequest) TickResult {
	res := TickResult{PRNumber: pr.Number}

	// Defense Layer 4 (loom v2): committer-allowlist guard. Refuses to
	// fast-forward main past a head commit whose committer is not in
	// the per-project allowlist. When no allowlist is configured the
	// guard is a no-op (preserves v1 behavior).
	if len(m.cfg.AllowedCommitterEmails) > 0 {
		ok, reason, err := m.committerAllowed(ctx, pr)
		if err != nil {
			m.cfg.Logger.Warn("merger: committer-check failed", "pr", pr.Number, "err", err)
			// Fail-closed: on lookup error, refuse the merge rather
			// than letting an unknown commit through.
			res.Status = "blocked"
			res.Detail = fmt.Sprintf("committer-allowlist check error: %v", err)
			return res
		}
		if !ok {
			res.Status = "blocked"
			res.Detail = reason
			return res
		}
	}

	// Run CI (skipped if no command configured — auto-merge unconditionally).
	if m.cfg.CICommand != "" {
		ok, output, err := m.runCI(ctx, pr)
		if err != nil {
			res.Status = "error"
			res.Detail = err.Error()
			return res
		}
		if !ok {
			res.Status = "ci-failed"
			res.Detail = tail(output, 4096)
			return res
		}
	}

	// Merge via Gitea. Gitea computes mergeability asynchronously after
	// a PR is created; if we hit 405 "Please try again later", give it
	// a moment and retry. Up to ~10s total before giving up.
	if err := m.mergeWithRetry(ctx, pr.Number); err != nil {
		res.Status = "error"
		res.Detail = err.Error()
		return res
	}

	// Read merged SHA from the post-merge main HEAD.
	sha, err := m.fetchMainSHA(ctx, pr.Number)
	if err != nil {
		// Best-effort: record empty SHA but keep status=merged.
		m.cfg.Logger.Warn("merger: post-merge SHA fetch", "err", err)
	}
	res.SHA = sha
	res.Status = "merged"

	agentID := agentIDFromHeadRef(pr.Head.Ref)
	if err := m.cfg.SyncLog.Append(projects.SyncEntry{
		Timestamp: time.Now().UTC(),
		SHA:       fallbackSHA(sha),
		PR:        pr.Number,
		AgentID:   agentID,
	}); err != nil {
		m.cfg.Logger.Warn("merger: synclog append", "err", err)
	}

	// Close the linked issue (PR title format: "...issue-#N..." or extract from branch).
	if issueNo := issueFromHeadRef(pr.Head.Ref); issueNo > 0 {
		if err := queue.Complete(ctx, m.cfg.Client, m.cfg.Project, issueNo); err != nil {
			m.cfg.Logger.Warn("merger: close issue", "issue", issueNo, "err", err)
		}
		// Honor push:origin per-issue label.
		if m.cfg.OriginPushFn != nil {
			issue, err := m.cfg.Client.GetIssue(ctx, projects.Owner, m.cfg.Project.Slug, issueNo)
			if err == nil && queue.HasLabel(issue, queue.LabelPushOrigin) && sha != "" {
				if err := m.cfg.OriginPushFn(ctx, sha); err != nil {
					m.cfg.Logger.Warn("merger: push:origin", "err", err)
				}
			}
		}
	}

	return res
}

// mergeWithRetry calls Gitea's MergePR endpoint, retrying briefly on
// "Please try again later" — Gitea computes PR mergeability in a
// background goroutine, so newly-opened PRs can race the merge call.
// Up to 10 attempts at 1s each (~10s wall-clock max).
func (m *Merger) mergeWithRetry(ctx context.Context, prNumber int64) error {
	const maxAttempts = 10
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		err := m.cfg.Client.MergePR(ctx, projects.Owner, m.cfg.Project.Slug, prNumber, "merge")
		if err == nil {
			return nil
		}
		lastErr = err
		// Only retry the specific "not ready" 405; everything else is fatal.
		if !strings.Contains(err.Error(), "Please try again later") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("merger: MergePR not ready after %d attempts: %w", maxAttempts, lastErr)
}

// runCI checks out the merge result of the PR head into a temp dir, runs
// the CI command, and reports (passed, output, error).
func (m *Merger) runCI(ctx context.Context, pr gitserver.PullRequest) (bool, string, error) {
	workdir, err := m.checkoutMerge(ctx, pr)
	if err != nil {
		return false, "", err
	}
	defer os.RemoveAll(workdir)

	timeout := m.cfg.CITimeout
	cictx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cictx, "sh", "-c", m.cfg.CICommand)
	cmd.Dir = workdir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(out), nil
	}
	return true, string(out), nil
}

// checkoutMerge clones admin/<slug> at HEAD of main, then merges the PR
// branch on top — yielding the prospective merge commit. Returns the
// path to the temp working tree.
func (m *Merger) checkoutMerge(ctx context.Context, pr gitserver.PullRequest) (string, error) {
	prDir := filepath.Join(m.cfg.WorkDir, fmt.Sprintf("pr-%d", pr.Number))
	_ = os.RemoveAll(prDir)
	if err := os.MkdirAll(prDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	// Use a token-authenticated URL for clone.
	authURL, err := injectToken(m.cfg.CloneURL, m.cfg.Token)
	if err != nil {
		return "", err
	}

	if _, err := m.ge.Run(ctx, prDir, "init"); err != nil {
		return "", fmt.Errorf("git init: %w", err)
	}
	if _, err := m.ge.Run(ctx, prDir, "remote", "add", "origin", authURL); err != nil {
		return "", err
	}
	// Configure a no-op identity so merge commits don't blow up on machines
	// without git config user.email set.
	_, _ = m.ge.Run(ctx, prDir, "config", "user.email", "merger@ycode.local")
	_, _ = m.ge.Run(ctx, prDir, "config", "user.name", "ycode-merger")

	if _, err := m.ge.Run(ctx, prDir, "fetch", "--depth=50", "origin", "main"); err != nil {
		return "", fmt.Errorf("fetch main: %w", err)
	}
	if _, err := m.ge.Run(ctx, prDir, "checkout", "-b", "main", "origin/main"); err != nil {
		return "", fmt.Errorf("checkout main: %w", err)
	}
	if _, err := m.ge.Run(ctx, prDir, "fetch", "--depth=50", "origin", pr.Head.Ref); err != nil {
		return "", fmt.Errorf("fetch %s: %w", pr.Head.Ref, err)
	}
	if _, err := m.ge.Run(ctx, prDir, "merge", "--no-edit", "FETCH_HEAD"); err != nil {
		// Merge conflict — return the dir with conflict markers; CI will fail naturally.
		// But for clarity, surface as an error.
		return "", fmt.Errorf("merge conflict on PR #%d: %w", pr.Number, err)
	}
	return prDir, nil
}

// fetchMainSHA reads main's tip SHA from the prospective-merge worktree.
func (m *Merger) fetchMainSHA(ctx context.Context, prNumber int64) (string, error) {
	if m.cfg.FetchMainSHAFn != nil {
		return m.cfg.FetchMainSHAFn(ctx, prNumber)
	}
	prDir := filepath.Join(m.cfg.WorkDir, fmt.Sprintf("pr-%d-sha", prNumber))
	_ = os.RemoveAll(prDir)
	if err := os.MkdirAll(prDir, 0o755); err != nil {
		return "", err
	}
	defer os.RemoveAll(prDir)

	authURL, err := injectToken(m.cfg.CloneURL, m.cfg.Token)
	if err != nil {
		return "", err
	}
	if _, err := m.ge.Run(ctx, prDir, "init"); err != nil {
		return "", err
	}
	if _, err := m.ge.Run(ctx, prDir, "remote", "add", "origin", authURL); err != nil {
		return "", err
	}
	if _, err := m.ge.Run(ctx, prDir, "fetch", "--depth=1", "origin", "main"); err != nil {
		return "", err
	}
	out, err := m.ge.Run(ctx, prDir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func injectToken(rawURL, token string) (string, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return rawURL, nil
	}
	scheme := "http://"
	rest := strings.TrimPrefix(rawURL, scheme)
	if rest == rawURL {
		scheme = "https://"
		rest = strings.TrimPrefix(rawURL, scheme)
	}
	return fmt.Sprintf("%stoken:%s@%s", scheme, token, rest), nil
}

var (
	branchIssueRe = regexp.MustCompile(`/issue-(\d+)$`)
	branchAgentRe = regexp.MustCompile(`^agent/(agent-[0-9A-Za-z._-]+)/`)
)

func issueFromHeadRef(ref string) int64 {
	m := branchIssueRe.FindStringSubmatch(ref)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

func agentIDFromHeadRef(ref string) string {
	m := branchAgentRe.FindStringSubmatch(ref)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

func fallbackSHA(s string) string {
	if len(s) == 40 {
		return s
	}
	// Synthetic placeholder when we couldn't read the real SHA.
	return "0000000000000000000000000000000000000000"
}
