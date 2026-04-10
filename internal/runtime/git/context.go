package git

import (
	"os/exec"
	"strings"
)

// Context holds git repository information.
type Context struct {
	IsRepo        bool
	Branch        string
	MainBranch    string
	User          string
	Status        string
	RecentCommits []string
	Diff          string // staged + unstaged diff snapshot
	StagedFiles   []string
}

// Discover detects git context for the given directory.
func Discover(dir string) *Context {
	ctx := &Context{}

	// Check if it's a git repo.
	if err := runGit(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return ctx
	}
	ctx.IsRepo = true

	// Current branch.
	if out, err := runGitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		ctx.Branch = strings.TrimSpace(out)
	}

	// Main branch detection.
	ctx.MainBranch = detectMainBranch(dir)

	// Git user.
	if out, err := runGitOutput(dir, "config", "user.name"); err == nil {
		ctx.User = strings.TrimSpace(out)
	}

	// Status (with --no-optional-locks to avoid lock contention, --branch for branch info).
	if out, err := runGitOutput(dir, "--no-optional-locks", "status", "--short", "--branch"); err == nil {
		ctx.Status = strings.TrimSpace(out)
	}

	// Recent commits.
	if out, err := runGitOutput(dir, "log", "--oneline", "-5"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line != "" {
				ctx.RecentCommits = append(ctx.RecentCommits, line)
			}
		}
	}

	// Diff snapshot (staged + unstaged).
	ctx.Diff = readGitDiff(dir)

	// Staged files.
	if out, err := runGitOutput(dir, "diff", "--cached", "--name-only"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line != "" {
				ctx.StagedFiles = append(ctx.StagedFiles, line)
			}
		}
	}

	return ctx
}

func readGitDiff(dir string) string {
	var sections []string

	if out, err := runGitOutput(dir, "diff", "--cached"); err == nil {
		trimmed := strings.TrimSpace(out)
		if trimmed != "" {
			sections = append(sections, "Staged changes:\n"+trimmed)
		}
	}

	if out, err := runGitOutput(dir, "diff"); err == nil {
		trimmed := strings.TrimSpace(out)
		if trimmed != "" {
			sections = append(sections, "Unstaged changes:\n"+trimmed)
		}
	}

	return strings.Join(sections, "\n\n")
}

func detectMainBranch(dir string) string {
	// Try common main branch names.
	for _, name := range []string{"main", "master"} {
		if err := runGit(dir, "rev-parse", "--verify", name); err == nil {
			return name
		}
	}
	return "main"
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func runGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
