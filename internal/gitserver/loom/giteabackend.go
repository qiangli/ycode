// Package loom contains the gitea-backed implementation of
// pkg/loom.Backend and the MCP adapter that exposes loom tools to
// foreign agentic coding tools over JSON-RPC.
//
// pkg/loom defines the substrate's public Go API; this package wires it
// to ycode's embedded Gitea via internal/gitserver primitives. See
// docs/loom.md for the user-facing contract.
package loom

import (
	"bytes"
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

	cgit "github.com/qiangli/coreutils/git"

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

	// Reference-clone sandbox config; see GiteaBackendOptions.
	giteaDataDir      string
	useReferenceClone bool
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

	// GiteaDataDir is the on-disk root Gitea's bare repos live under
	// (typically ~/.agents/ycode/observability/gitea). Required only
	// when UseReferenceClone is true; otherwise ignored.
	GiteaDataDir string

	// UseReferenceClone, when true, switches PrepareSandbox to
	// `git clone --reference <bare-on-disk> <gitea-http-url>`. Spike
	// 3 validated the per-clone-refs / shared-object-store property
	// empirically (docs/loom-v2-implementation.md). Default false —
	// behavior unchanged until operators opt in via .ycode/loom.yaml
	// backend.use_reference_clone: true.
	//
	// Operational constraint: the substrate must NOT git-gc the
	// parent bare while any reference-clone child is alive. The lease
	// store doubles as the liveness tracker for this guard.
	UseReferenceClone bool
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
	if opts.UseReferenceClone && opts.GiteaDataDir == "" {
		return nil, errors.New("loom: UseReferenceClone requires GiteaDataDir")
	}
	return &GiteaBackend{
		client:            opts.Client,
		registry:          opts.Registry,
		token:             opts.Token,
		log:               logger,
		onProjectActive:   opts.OnProjectActive,
		byCwd:             map[string]projectEntry{},
		bySlug:            map[string]projectEntry{},
		giteaDataDir:      opts.GiteaDataDir,
		useReferenceClone: opts.UseReferenceClone,
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

	// Reference-clone seam (v2, opt-in). When enabled, skip the
	// collab full-clone path and lay out a sandbox whose .git/objects
	// shares with the bare repo via `git clone --reference`. Spike 3
	// validated the per-clone-refs / shared-objects property. The
	// fallback (UseReferenceClone=false) preserves v1's behavior
	// byte-for-byte.
	if b.useReferenceClone {
		path, err := prepareReferenceCloneSandbox(ctx, sandboxRoot, b.giteaDataDir, slug, branch, agentID, cloneURL, b.token)
		if err != nil {
			return "", err
		}
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
	// rather than parsing exit codes (clearer and stable). gitRun is
	// pure-Go-first and falls back to host git for the conflict case,
	// where git leaves a conflicted worktree the probe below detects.
	rebaseStdout, rebaseStderr, rebaseCode, rebaseRunErr := gitRun(ctx, sandboxPath, []string{"rebase", "origin/" + baseBranch})
	rebaseFailed := rebaseRunErr != nil || rebaseCode != 0
	rebaseDetail := strings.TrimSpace(rebaseStderr)
	if rebaseDetail == "" {
		rebaseDetail = strings.TrimSpace(rebaseStdout)
	}

	// Probe for conflicts regardless of rebase exit status.
	conflicts, err := captureGit(ctx, sandboxPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		// If even `git diff` fails the sandbox is in a weird state;
		// surface the original rebase error if any.
		if rebaseFailed {
			return nil, fmt.Errorf("loom: rebase: %v: %s", rebaseRunErr, rebaseDetail)
		}
		return nil, fmt.Errorf("loom: detect rebase conflicts: %w", err)
	}
	files := splitConflictFiles(conflicts)
	if len(files) > 0 {
		// Conflicts present — return them; rebase exit is expected non-zero.
		return files, nil
	}
	if rebaseFailed {
		// Non-zero exit with no conflict files means a real failure.
		return nil, fmt.Errorf("loom: rebase origin/%s: %v: %s", baseBranch, rebaseRunErr, rebaseDetail)
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

// gitRun runs a git subcommand pure-Go-first: it tries the in-process
// coreutils/git layer and only falls back to the host git binary when the
// pure-Go layer declines the case (ErrUnsupported) — e.g. a rebase that
// hits conflicts, which git resolves by leaving a conflicted worktree the
// pure-Go linear rebase doesn't model. A non-zero exit is returned via
// exitCode (not as a Go error); err is reserved for spawn/internal
// failures so callers can interpret command-level failures themselves.
func gitRun(ctx context.Context, dir string, args []string) (stdout, stderr string, exitCode int, err error) {
	res, ferr := cgit.Exec(ctx, dir, args)
	if ferr == nil {
		return res.Stdout, res.Stderr, res.ExitCode, nil
	}
	if !errors.Is(ferr, cgit.ErrUnsupported) {
		return "", "", -1, ferr
	}
	// Pure-Go layer can't handle this case; defer to the host git binary.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	rerr := cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if rerr != nil {
		var ee *exec.ExitError
		if errors.As(rerr, &ee) {
			return outb.String(), errb.String(), code, nil
		}
		return outb.String(), errb.String(), -1, rerr
	}
	return outb.String(), errb.String(), 0, nil
}

// runGit executes a git subcommand in dir, discarding stdout. Returns any
// stderr in the error. Pure-Go-first via gitRun.
func runGit(ctx context.Context, dir string, args ...string) error {
	out, errOut, code, err := gitRun(ctx, dir, args)
	if err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if code != 0 {
		detail := strings.TrimSpace(errOut)
		if detail == "" {
			detail = strings.TrimSpace(out)
		}
		return fmt.Errorf("git %s: exit %d: %s", strings.Join(args, " "), code, detail)
	}
	return nil
}

// captureGit runs a git subcommand and returns its stdout. Pure-Go-first
// via gitRun.
func captureGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, errOut, code, err := gitRun(ctx, dir, args)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit %d: %s", strings.Join(args, " "), code, strings.TrimSpace(errOut))
	}
	return out, nil
}

// hasStagedChanges reports whether `git diff --cached` would produce
// any output. Used to skip empty commits in CommitAndPush.
func hasStagedChanges(ctx context.Context, dir string) (bool, error) {
	// Pure-Go-first via captureGit; list staged paths and check for any.
	// (Avoids depending on `diff --quiet`'s exit-code-1-means-changes
	// convention, which not every git layer mirrors.)
	out, err := captureGit(ctx, dir, "diff", "--cached", "--name-only")
	if err != nil {
		return false, fmt.Errorf("git diff --cached: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}
