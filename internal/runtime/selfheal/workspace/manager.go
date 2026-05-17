// Package workspace handles the external-repo clone + worktree
// lifecycle for selfheal workers (Phase 3 of the plan). Each
// signature gets its own per-fix workspace under
// ~/.agents/ycode/selfheal/<signature>/ so parallel workers never
// collide.
//
// Layout:
//
//	~/.agents/ycode/selfheal/<signature>/
//	  ycode/                 # bare-ish clone of the target ycode repo
//	  worktrees/autoloop/    # working branch this fix lives on
//	  trace/                 # captured failing tool invocations (Phase 5)
//	  iterations/            # per-iteration autoloop logs (Phase 4)
//	  outcome.json           # final status + PR URL or local-only ref (Phase 5)
package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/git"
)

// Layout encapsulates per-signature paths so callers don't reimplement
// the path arithmetic. Always construct via PathsFor.
type Layout struct {
	Root         string // ~/.agents/ycode/selfheal/<signature>
	Clone        string // <Root>/ycode — the upstream clone
	WorktreeRoot string // <Root>/worktrees
	TracePath    string // <Root>/trace
	IterPath     string // <Root>/iterations
	Outcome      string // <Root>/outcome.json
}

// PathsFor returns the Layout for signature beneath baseDir
// (typically ~/.agents/ycode/selfheal).
func PathsFor(baseDir, signature string) Layout {
	root := filepath.Join(baseDir, signature)
	return Layout{
		Root:         root,
		Clone:        filepath.Join(root, "ycode"),
		WorktreeRoot: filepath.Join(root, "worktrees"),
		TracePath:    filepath.Join(root, "trace"),
		IterPath:     filepath.Join(root, "iterations"),
		Outcome:      filepath.Join(root, "outcome.json"),
	}
}

// Manager performs the git operations needed to set up and tear down
// per-signature workspaces. Stateless — safe to share across goroutines.
type Manager struct {
	exec *git.GitExec
}

// New returns a Manager that shells out to git directly (no toolexec
// container fallback — selfheal runs on the operator's host where
// git is always present).
func New() *Manager {
	return &Manager{exec: git.NewGitExec(nil)}
}

// EnsureClone clones repoURL into layout.Clone if absent, or runs
// `git fetch --all --prune` if a clone already exists at that path.
// Returns nil with no work done when the clone is already at the
// requested URL — the cheap, idempotent case operators hit on every
// re-attempted fix.
func (m *Manager) EnsureClone(ctx context.Context, layout Layout, repoURL string) error {
	if repoURL == "" {
		return errors.New("workspace: empty repoURL")
	}
	if err := os.MkdirAll(filepath.Dir(layout.Clone), 0o755); err != nil {
		return fmt.Errorf("workspace: mkdir clone parent: %w", err)
	}
	// Already cloned?
	if info, err := os.Stat(filepath.Join(layout.Clone, ".git")); err == nil && info.IsDir() {
		// Sanity-check the origin URL matches; refuse to mix repos
		// in the same per-signature workspace.
		curURL, err := m.exec.RunOutput(ctx, layout.Clone, "config", "--get", "remote.origin.url")
		if err == nil && curURL != "" && curURL != repoURL {
			return fmt.Errorf("workspace: clone at %s tracks %q but caller requested %q", layout.Clone, curURL, repoURL)
		}
		return m.exec.RunCheck(ctx, layout.Clone, "fetch", "--all", "--prune")
	}
	// Fresh clone. Use --filter=blob:none + --no-checkout for speed:
	// the worktree-add immediately after will materialize what's
	// needed, and we avoid pulling history for files autoloop will
	// never touch in this signature's narrow fix.
	args := []string{"clone", "--filter=blob:none", "--no-checkout", repoURL, layout.Clone}
	return m.exec.RunCheck(ctx, "", args...)
}

// CreateWorktree adds a fresh worktree on a new branch off the clone's
// default upstream HEAD. Returns the absolute worktree path. If the
// branch already exists (re-attempt path) the existing worktree is
// returned without re-creating it — preserves in-progress autoloop
// state on retry.
func (m *Manager) CreateWorktree(ctx context.Context, layout Layout, branch string) (string, error) {
	if branch == "" {
		return "", errors.New("workspace: empty branch")
	}
	wt := filepath.Join(layout.WorktreeRoot, sanitizeForFilename(branch))
	if _, err := os.Stat(wt); err == nil {
		// Already materialized — sanity check it's a real worktree.
		if _, err := m.exec.RunOutput(ctx, wt, "rev-parse", "--git-dir"); err == nil {
			return wt, nil
		}
		// Stale leftover; remove so the create succeeds.
		_ = os.RemoveAll(wt)
	}
	if err := os.MkdirAll(layout.WorktreeRoot, 0o755); err != nil {
		return "", fmt.Errorf("workspace: mkdir worktree root: %w", err)
	}
	// Resolve the upstream HEAD ref so the new branch starts from
	// the latest tip (origin/HEAD), not from a stale local branch.
	upstreamRef := "origin/HEAD"
	if _, err := m.exec.RunOutput(ctx, layout.Clone, "rev-parse", "--verify", upstreamRef); err != nil {
		upstreamRef = "origin/main"
	}
	if err := m.exec.RunCheck(ctx, layout.Clone, "worktree", "add", "-b", branch, wt, upstreamRef); err != nil {
		return "", fmt.Errorf("workspace: worktree add: %w", err)
	}
	return wt, nil
}

// RemoveWorktree tears down a per-signature worktree (and its branch).
// Best-effort: removal failures during cleanup don't block subsequent
// fixes — the next CreateWorktree will RemoveAll the directory.
func (m *Manager) RemoveWorktree(ctx context.Context, layout Layout, branch string) error {
	wt := filepath.Join(layout.WorktreeRoot, sanitizeForFilename(branch))
	if _, err := os.Stat(wt); err != nil {
		return nil
	}
	if err := m.exec.RunCheck(ctx, layout.Clone, "worktree", "remove", "--force", wt); err != nil {
		// Fall back to plain RemoveAll for the corrupted-worktree case.
		_ = os.RemoveAll(wt)
	}
	_ = m.exec.RunCheck(ctx, layout.Clone, "branch", "-D", branch)
	return nil
}

// DiscoverFork attempts to find a fork of the ycode source the
// operator built from. Resolution order:
//
//  1. SELFHEAL_REPO env var (escape hatch for tests + power users)
//  2. runtime/debug.ReadBuildInfo "vcs.remote" (set by go build -buildvcs=true,
//     the default since Go 1.18) — points at the repo this binary
//     was built from
//  3. Caller-provided fallbackDir's origin remote, if non-empty
//  4. Upstream qiangli/ycode
//
// Returns the chosen URL. Error only if no candidate is usable —
// the upstream fallback effectively makes that case unreachable in
// practice.
func (m *Manager) DiscoverFork(ctx context.Context, fallbackDir string) (string, error) {
	const upstream = "https://github.com/qiangli/ycode.git"

	if v := strings.TrimSpace(os.Getenv("SELFHEAL_REPO")); v != "" {
		return v, nil
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.remote" && s.Value != "" {
				return normalizeRepoURL(s.Value), nil
			}
		}
	}
	if fallbackDir != "" {
		if out, err := m.exec.RunOutput(ctx, fallbackDir, "config", "--get", "remote.origin.url"); err == nil && out != "" {
			return normalizeRepoURL(out), nil
		}
	}
	return upstream, nil
}

// normalizeRepoURL rewrites git@host:owner/repo.git into
// https://host/owner/repo.git so the operator's https GitHub token
// works for the subsequent push. Leaves https URLs untouched.
func normalizeRepoURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "git@") {
		// git@github.com:owner/repo.git → https://github.com/owner/repo.git
		rest := strings.TrimPrefix(s, "git@")
		if idx := strings.Index(rest, ":"); idx >= 0 {
			host := rest[:idx]
			path := rest[idx+1:]
			return "https://" + host + "/" + path
		}
	}
	// ssh:// → https://
	if u, err := url.Parse(s); err == nil && u.Scheme == "ssh" {
		path := strings.TrimPrefix(u.Path, "/")
		return "https://" + u.Host + "/" + path
	}
	return s
}

// sanitizeForFilename keeps the worktree path predictable across
// platforms even when the branch name carries characters Git accepts
// but filesystems don't. Lowercase alnum + dash.
func sanitizeForFilename(s string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "branch"
	}
	return out
}
