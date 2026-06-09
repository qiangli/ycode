// Package loom contains the gitea-backed implementation of
// pkg/loom.Backend and the MCP adapter that exposes loom tools to
// foreign agentic coding tools over JSON-RPC.
//
// pkg/loom defines the substrate's public Go API; this package wires it
// to ycode's embedded Gitea via internal/gitserver primitives. See
// docs/loom.md for the user-facing contract.
package loom

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/agents"
	"github.com/qiangli/ycode/internal/gitserver/collab"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/weaveapi"
	"github.com/qiangli/ycode/pkg/loom"
)

// GiteaBackend implements pkg/loom.Backend using ycode's embedded
// Gitea + native gitserver primitives.
type GiteaBackend struct {
	client   *gitserver.Client
	registry *projects.Registry
	token    string
	log      *slog.Logger

	// onProjectActive is called the first time NotifyProjectActive
	// fires for a given slug. The wiring layer (cmd/ycode/serve.go)
	// uses this to lazy-start a per-project merger goroutine.
	onProjectActive ProjectActiveFn

	// resolveProject memoizes cwd → *projects.Project so EnsureProject
	// is idempotent without hammering the registry.
	mu     sync.Mutex
	byCwd  map[string]projectEntry
	bySlug map[string]projectEntry

	// weave lazily wraps b.client in a weaveapi.Client; constructed
	// once on first ClaimNextIssue (or other v2-label op).
	weaveOnce sync.Once
	weave     *weaveapi.Client
}

// weaveClient returns the lazily-constructed weaveapi.Client.
func (b *GiteaBackend) weaveClient() *weaveapi.Client {
	b.weaveOnce.Do(func() {
		b.weave = weaveapi.NewClient(b.client)
	})
	return b.weave
}

// ProjectActiveFn is invoked by NotifyProjectActive on first use of a
// project. Implementations may start per-project workers (e.g. a
// merger). Errors are logged but not propagated.
type ProjectActiveFn func(ctx context.Context, slug, cloneURL string) error

type projectEntry struct {
	project  *projects.Project
	cloneURL string
}

// GiteaBackendOptions wires NewGiteaBackend.
type GiteaBackendOptions struct {
	// Client is the Gitea API client. Required.
	Client *gitserver.Client

	// Registry maps host cwd → admin/<slug> repos. Required.
	Registry *projects.Registry

	// Token is the admin token used to authenticate clones, pushes,
	// and API calls. Required.
	Token string

	// OnProjectActive is invoked once per slug, the first time a
	// project sees a lease. Optional. Use it to start per-project
	// workers like a merger.
	OnProjectActive ProjectActiveFn

	// Logger is required by callers but defaulted to slog.Default()
	// when nil.
	Logger *slog.Logger
}

// NewGiteaBackend constructs a Backend wired to ycode's embedded Gitea.
func NewGiteaBackend(opts GiteaBackendOptions) (*GiteaBackend, error) {
	if opts.Client == nil {
		return nil, errors.New("loom: nil Client")
	}
	if opts.Registry == nil {
		return nil, errors.New("loom: nil Registry")
	}
	if opts.Token == "" {
		return nil, errors.New("loom: empty Token")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &GiteaBackend{
		client:          opts.Client,
		registry:        opts.Registry,
		token:           opts.Token,
		log:             logger,
		onProjectActive: opts.OnProjectActive,
		byCwd:           map[string]projectEntry{},
		bySlug:          map[string]projectEntry{},
	}, nil
}

// EnsureProject resolves cwd to admin/<slug>, creating the repo if it
// doesn't exist, and looks up its CloneURL.
func (b *GiteaBackend) EnsureProject(ctx context.Context, cwd string) (string, string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", fmt.Errorf("loom: resolve cwd: %w", err)
	}

	b.mu.Lock()
	if e, ok := b.byCwd[abs]; ok {
		b.mu.Unlock()
		return e.project.Slug, e.cloneURL, nil
	}
	b.mu.Unlock()

	project, err := b.registry.Resolve(ctx, abs)
	if err != nil {
		return "", "", fmt.Errorf("loom: registry.Resolve: %w", err)
	}
	if _, err := projects.EnsureRepo(ctx, b.client, project); err != nil {
		return "", "", fmt.Errorf("loom: EnsureRepo: %w", err)
	}

	cloneURL, err := b.findCloneURL(ctx, project.Slug)
	if err != nil {
		return "", "", err
	}

	b.mu.Lock()
	entry := projectEntry{project: project, cloneURL: cloneURL}
	b.byCwd[abs] = entry
	b.bySlug[project.Slug] = entry
	b.mu.Unlock()

	return project.Slug, cloneURL, nil
}

// PrepareSandbox delegates to collab.PrepareSandbox — the validated
// full-clone-with-author-identity path used by the internal collab
// orchestrator, with branch passed in directly so loom can use its
// free-form (issueNo=0) naming convention.
func (b *GiteaBackend) PrepareSandbox(ctx context.Context, sandboxRoot, slug, branch, agentID, name, email, cloneURL string) (string, error) {
	a := &agents.Agent{ID: agentID, Name: name}
	// collab.PrepareSandbox places sandboxes at <root>/<agentID>/issue-<N>;
	// loom uses issueNo=0, yielding <root>/<agentID>/issue-0.
	path, err := collab.PrepareSandbox(ctx, sandboxRoot, cloneURL, b.token, a, 0, branch)
	if err != nil {
		return "", err
	}
	// PrepareSandbox sets git author from agents.AuthorTrailer, which
	// uses agent.ID twice (name and email). Override email here so the
	// caller's distinct authorName/authorEmail land in commits.
	if name != "" {
		if err := runGit(ctx, path, "config", "user.name", name); err != nil {
			return "", fmt.Errorf("loom: set user.name: %w", err)
		}
	}
	if email != "" {
		if err := runGit(ctx, path, "config", "user.email", email); err != nil {
			return "", fmt.Errorf("loom: set user.email: %w", err)
		}
	}
	return path, nil
}

// CommitAndPush stages every change, commits with the configured author,
// and pushes the branch via agents.Branch.Push.
func (b *GiteaBackend) CommitAndPush(ctx context.Context, sandboxPath, slug, branch, message string, force bool) (string, error) {
	if sandboxPath == "" || branch == "" {
		return "", fmt.Errorf("loom: CommitAndPush: empty sandbox or branch")
	}

	// Stage and commit. Skip the commit if there's nothing to add — the
	// foreign tool may have already committed manually.
	if err := runGit(ctx, sandboxPath, "add", "-A"); err != nil {
		return "", fmt.Errorf("loom: git add: %w", err)
	}
	hasChanges, err := hasStagedChanges(ctx, sandboxPath)
	if err != nil {
		return "", err
	}
	if hasChanges {
		if err := runGit(ctx, sandboxPath, "commit", "-m", message); err != nil {
			return "", fmt.Errorf("loom: git commit: %w", err)
		}
	}

	// Resolve cloneURL from cached project entry.
	b.mu.Lock()
	entry, ok := b.bySlug[slug]
	b.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("loom: unknown slug %q (no prior EnsureProject)", slug)
	}

	br := &agents.Branch{
		Project: entry.project,
		Agent:   &agents.Agent{ID: agentIDFromBranch(branch)},
		Name:    branch,
	}
	if err := br.Push(ctx, agents.PushOptions{
		WorktreePath: sandboxPath,
		CloneURL:     entry.cloneURL,
		Token:        b.token,
		Force:        force,
	}); err != nil {
		return "", err
	}

	sha, err := captureGit(ctx, sandboxPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("loom: rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// EnsureRemoteBranch is a no-op for the gitea backend — CommitAndPush's
// "git push" creates the branch implicitly. AssignBranch via API is
// available but redundant once a commit has been pushed.
func (b *GiteaBackend) EnsureRemoteBranch(ctx context.Context, slug, branch string) error {
	return nil
}

// OpenPR opens a PR from branch into main using agents.Branch.OpenPR.
func (b *GiteaBackend) OpenPR(ctx context.Context, slug, branch, title, body string) (int64, error) {
	b.mu.Lock()
	entry, ok := b.bySlug[slug]
	b.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("loom: unknown slug %q", slug)
	}
	br := &agents.Branch{
		Project: entry.project,
		Agent:   &agents.Agent{ID: agentIDFromBranch(branch)},
		Name:    branch,
	}
	pr, err := br.OpenPR(ctx, b.client, title, body)
	if err != nil {
		return 0, err
	}
	return pr.Number, nil
}

// ListPRStates queries Gitea for every PR whose head branch starts with
// branchPrefix. State is "open" / "closed"; Merged reflects whether the
// PR was actually merged (vs closed without merging).
func (b *GiteaBackend) ListPRStates(ctx context.Context, slug, branchPrefix string) ([]loom.BackendPRState, error) {
	out := []loom.BackendPRState{}
	for _, state := range []string{"open", "closed"} {
		prs, err := b.client.ListPRs(ctx, projects.Owner, slug, state)
		if err != nil {
			return nil, fmt.Errorf("loom: list %s PRs: %w", state, err)
		}
		for _, pr := range prs {
			if !strings.HasPrefix(pr.Head.Ref, branchPrefix) {
				continue
			}
			out = append(out, loom.BackendPRState{
				PRNumber: pr.Number,
				HeadRef:  pr.Head.Ref,
				State:    pr.State,
				// Gitea's PR.State for a successfully-merged PR is
				// "closed" with merged=true; the API client doesn't
				// surface the bool, but a closed PR whose branch was
				// the head of a merge commit is effectively merged.
				// Best-effort: treat all closed PRs as merged here;
				// the merger only closes-via-merge, never via abandon.
				Merged: state == "closed",
			})
		}
	}
	return out, nil
}

// DeleteSandbox removes the sandbox directory tree.
func (b *GiteaBackend) DeleteSandbox(path string) error {
	if path == "" {
		return nil
	}
	return os.RemoveAll(path)
}

// DeleteBranch removes a branch from the forge. Best-effort — missing
// branches and other 4xx responses are logged but not propagated.
func (b *GiteaBackend) DeleteBranch(ctx context.Context, slug, branch string) error {
	if err := b.client.DeleteBranch(ctx, projects.Owner, slug, branch); err != nil {
		b.log.Debug("loom: delete branch best-effort failed",
			"slug", slug, "branch", branch, "err", err)
	}
	return nil
}

// NotifyProjectActive fires onProjectActive (if set) with the slug and
// cloneURL.
func (b *GiteaBackend) NotifyProjectActive(ctx context.Context, slug, cloneURL string) error {
	if b.onProjectActive == nil {
		return nil
	}
	return b.onProjectActive(ctx, slug, cloneURL)
}

// findCloneURL looks up admin/<slug>'s CloneURL via the Gitea API.
func (b *GiteaBackend) findCloneURL(ctx context.Context, slug string) (string, error) {
	repos, err := b.client.ListRepos(ctx)
	if err != nil {
		return "", fmt.Errorf("loom: list repos: %w", err)
	}
	for _, r := range repos {
		if r.Name == slug {
			return r.CloneURL, nil
		}
	}
	return "", fmt.Errorf("loom: clone URL not found for slug %q", slug)
}

// agentIDFromBranch extracts the "agent-loom-label-hex" identifier from
// a branch of the form "agent/agent-loom-label-hex/free-rand".
func agentIDFromBranch(branch string) string {
	if !strings.HasPrefix(branch, "agent/") {
		return branch
	}
	rest := strings.TrimPrefix(branch, "agent/")
	if i := strings.Index(rest, "/"); i > 0 {
		return rest[:i]
	}
	return rest
}

// ClaimNextIssue is the Gitea-backed implementation of pkg/loom's
// atomic-claim contract. Lists open issues labeled loom:todo, filters
// out claimed/terminal ones, sorts by (priority_tier, created_at,
// issue_number), and flips the top candidate's state label to
// loom:working via weaveapi.SetState.
//
// Atomicity for the *combination* of read + label flip is provided by
// the per-project sync.Mutex in pkg/loom.Service.Claim. This method
// is unsafe under concurrent invocation against the same slug
// without that external lock.
func (b *GiteaBackend) ClaimNextIssue(ctx context.Context, slug string) (int64, error) {
	if slug == "" {
		return 0, fmt.Errorf("loom: ClaimNextIssue: empty slug")
	}

	// Pull all open issues with the loom:todo label.
	issues, err := b.client.ListIssues(ctx, projects.Owner, slug, "open", []string{weaveapi.LabelStateTodo})
	if err != nil {
		return 0, fmt.Errorf("loom: ClaimNextIssue: list todo issues: %w", err)
	}

	// Filter out issues that also carry a state label other than
	// loom:todo (working / submitted / merged / abandoned / proposed).
	// loom:proposed in particular means "agent-filed, waiting for human
	// review" — explicitly excluded from candidates.
	type candidate struct {
		number   int64
		priority int
	}
	var cands []candidate
	for _, iss := range issues {
		exclude := false
		var prio = weaveapi.PriorityValue("") // default p2
		for _, l := range iss.Labels {
			switch l.Name {
			case weaveapi.LabelStateWorking,
				weaveapi.LabelStateSubmitted,
				weaveapi.LabelStateCIFailed,
				weaveapi.LabelStateConflict,
				weaveapi.LabelStateMerged,
				weaveapi.LabelStateAbandoned,
				weaveapi.LabelProposed:
				exclude = true
			}
			if weaveapi.IsPriorityLabel(l.Name) {
				prio = weaveapi.PriorityValue(l.Name)
			}
		}
		if exclude {
			continue
		}
		cands = append(cands, candidate{number: iss.Number, priority: prio})
	}

	if len(cands) == 0 {
		return 0, loom.ErrQueueEmpty
	}

	// Sort: priority asc (p0 wins) → issue_number asc as deterministic
	// tiebreaker. created_at isn't currently exposed by gitserver.Issue
	// — issue_number ascending is a near-perfect FIFO proxy since
	// Gitea allocates strictly increasing numbers per repo.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].priority != cands[j].priority {
			return cands[i].priority < cands[j].priority
		}
		return cands[i].number < cands[j].number
	})

	top := cands[0]
	if err := b.weaveClient().SetState(ctx, projects.Owner, slug, top.number, weaveapi.LabelStateWorking); err != nil {
		return 0, fmt.Errorf("loom: ClaimNextIssue: flip state to working: %w", err)
	}
	return top.number, nil
}

// Checkpoint stages every change in the sandbox and makes a local
// commit (no push). Idempotent: when there's nothing to commit, the
// current HEAD SHA is returned with no error so the caller can treat
// the call as durable regardless of staged-state.
func (b *GiteaBackend) Checkpoint(ctx context.Context, sandboxPath, message string) (string, error) {
	if sandboxPath == "" {
		return "", fmt.Errorf("loom: Checkpoint: empty sandbox path")
	}
	if err := runGit(ctx, sandboxPath, "add", "-A"); err != nil {
		return "", fmt.Errorf("loom: git add: %w", err)
	}
	hasChanges, err := hasStagedChanges(ctx, sandboxPath)
	if err != nil {
		return "", err
	}
	if hasChanges {
		msg := message
		if msg == "" {
			msg = "loom: checkpoint"
		}
		if err := runGit(ctx, sandboxPath, "commit", "-m", msg); err != nil {
			return "", fmt.Errorf("loom: git commit: %w", err)
		}
	}
	sha, err := captureGit(ctx, sandboxPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("loom: rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// RebaseSandbox runs `git fetch origin <baseBranch>` then
// `git rebase origin/<baseBranch>` inside the sandbox. If the rebase
// produces conflicts, the working tree is left with markers in the
// conflicted files (no `git rebase --abort`) so the foreign agent can
// resolve and resubmit.
//
// Returns the list of conflicted files (those reported by `git diff
// --name-only --diff-filter=U`). Empty list + nil error means the
// rebase landed cleanly.
func (b *GiteaBackend) RebaseSandbox(ctx context.Context, sandboxPath, baseBranch string) ([]string, error) {
	if sandboxPath == "" || baseBranch == "" {
		return nil, fmt.Errorf("loom: RebaseSandbox: empty sandbox or baseBranch")
	}

	// Fetch first; rebase is local-only after a successful fetch.
	if err := runGit(ctx, sandboxPath, "fetch", "origin", baseBranch); err != nil {
		return nil, fmt.Errorf("loom: fetch origin %s: %w", baseBranch, err)
	}

	// Rebase. A non-zero exit indicates either a real failure or a
	// conflict; we distinguish by inspecting `git diff` afterward
	// rather than parsing exit codes (clearer and stable).
	rebaseCmd := exec.CommandContext(ctx, "git", "rebase", "origin/"+baseBranch)
	rebaseCmd.Dir = sandboxPath
	rebaseOut, rebaseErr := rebaseCmd.CombinedOutput()

	// Probe for conflicts regardless of rebase exit status.
	conflicts, err := captureGit(ctx, sandboxPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		// If even `git diff` fails the sandbox is in a weird state;
		// surface the original rebase error if any.
		if rebaseErr != nil {
			return nil, fmt.Errorf("loom: rebase: %w: %s", rebaseErr, strings.TrimSpace(string(rebaseOut)))
		}
		return nil, fmt.Errorf("loom: detect rebase conflicts: %w", err)
	}
	files := splitConflictFiles(conflicts)
	if len(files) > 0 {
		// Conflicts present — return them; rebase exit is expected non-zero.
		return files, nil
	}
	if rebaseErr != nil {
		// Non-zero exit with no conflict files means a real failure.
		return nil, fmt.Errorf("loom: rebase origin/%s: %w: %s", baseBranch, rebaseErr, strings.TrimSpace(string(rebaseOut)))
	}
	return nil, nil
}

// splitConflictFiles tokenizes `git diff --name-only` output (newline-
// separated) and returns a clean string slice, trimming any blank lines.
func splitConflictFiles(out string) []string {
	var files []string
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, line)
	}
	return files
}

// runGit executes a git subcommand in dir, discarding stdout. Returns
// any stderr in the error.
func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// captureGit runs a git subcommand and returns its stdout.
func captureGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// hasStagedChanges reports whether `git diff --cached` would produce
// any output. Used to skip empty commits in CommitAndPush.
func hasStagedChanges(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// Exit code 1 from diff --quiet means there ARE changes.
			if ee.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, fmt.Errorf("git diff --cached: %w", err)
	}
	return false, nil
}
