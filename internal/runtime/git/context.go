package git

import (
	"context"
	"strings"
)

// Context holds git repository information.
type Context struct {
	IsRepo        bool
	Root          string // absolute path to the git worktree root
	Branch        string
	MainBranch    string
	User          string
	Status        string
	RecentCommits []string
	Diff          string // staged + unstaged diff snapshot
	StagedFiles   []string
}

// defaultExec is used when callers don't provide a GitExec.
// Uses direct os/exec with no container fallback.
var defaultExec = NewGitExec(nil)

// Discover detects git context for the given directory using direct exec.
func Discover(dir string) *Context {
	return DiscoverWith(context.Background(), dir, defaultExec)
}

// DiscoverWith detects git context using the provided GitExec,
// enabling three-tier fallback (native → host exec → container).
func DiscoverWith(ctx context.Context, dir string, ge *GitExec) *Context {
	gc := &Context{}

	// Check if it's a git repo.
	if err := ge.RunCheck(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return gc
	}
	gc.IsRepo = true

	// Git worktree root (toplevel).
	if out, err := ge.RunOutput(ctx, dir, "rev-parse", "--show-toplevel"); err == nil {
		gc.Root = out
	}

	// Current branch.
	if out, err := ge.RunOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		gc.Branch = out
	}

	// Main branch detection.
	gc.MainBranch = detectMainBranchWith(ctx, dir, ge)

	// Git user.
	if out, err := ge.RunOutput(ctx, dir, "config", "user.name"); err == nil {
		gc.User = out
	}

	// Status (with --no-optional-locks to avoid lock contention, --branch for branch info).
	if out, err := ge.RunOutput(ctx, dir, "--no-optional-locks", "status", "--short", "--branch"); err == nil {
		gc.Status = out
	}

	// Recent commits.
	if out, err := ge.RunOutput(ctx, dir, "log", "--oneline", "-5"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if line != "" {
				gc.RecentCommits = append(gc.RecentCommits, line)
			}
		}
	}

	// Diff snapshot (staged + unstaged).
	gc.Diff = readGitDiffWith(ctx, dir, ge)

	// Staged files.
	if out, err := ge.RunOutput(ctx, dir, "diff", "--cached", "--name-only"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if line != "" {
				gc.StagedFiles = append(gc.StagedFiles, line)
			}
		}
	}

	return gc
}

func readGitDiffWith(ctx context.Context, dir string, ge *GitExec) string {
	var sections []string

	if out, err := ge.RunOutput(ctx, dir, "diff", "--cached"); err == nil && out != "" {
		sections = append(sections, "Staged changes:\n"+out)
	}

	if out, err := ge.RunOutput(ctx, dir, "diff"); err == nil && out != "" {
		sections = append(sections, "Unstaged changes:\n"+out)
	}

	return strings.Join(sections, "\n\n")
}

// MergeBase returns the best common ancestor between two commits/branches.
func MergeBase(dir, ref1, ref2 string) (string, error) {
	return MergeBaseWith(context.Background(), dir, defaultExec, ref1, ref2)
}

// MergeBaseWith returns the merge base using the provided GitExec.
func MergeBaseWith(ctx context.Context, dir string, ge *GitExec, ref1, ref2 string) (string, error) {
	return ge.RunOutput(ctx, dir, "merge-base", ref1, ref2)
}

// DiffStat returns the diff stat between two refs.
func DiffStat(dir, base, head string) (string, error) {
	return DiffStatWith(context.Background(), dir, defaultExec, base, head)
}

// DiffStatWith returns the diff stat using the provided GitExec.
func DiffStatWith(ctx context.Context, dir string, ge *GitExec, base, head string) (string, error) {
	if base == "" {
		main := detectMainBranchWith(ctx, dir, ge)
		var err error
		base, err = MergeBaseWith(ctx, dir, ge, main, head)
		if err != nil {
			return "", err
		}
	}
	return ge.RunOutput(ctx, dir, "diff", "--stat", base+".."+head)
}

func detectMainBranchWith(ctx context.Context, dir string, ge *GitExec) string {
	for _, name := range []string{"main", "master"} {
		if err := ge.RunCheck(ctx, dir, "rev-parse", "--verify", name); err == nil {
			return name
		}
	}
	return "main"
}
