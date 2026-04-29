package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeConfig configures worktree creation.
type WorktreeConfig struct {
	BaseDir    string // directory for worktrees (e.g., .agents/ycode/worktrees/)
	BaseBranch string // branch to base the worktree on (default: current branch)
}

// Worktree represents an active git worktree.
type Worktree struct {
	Path   string // filesystem path to the worktree
	Branch string // branch name
}

// CreateWorktree creates a new git worktree for isolated work.
func CreateWorktree(repoDir string, cfg WorktreeConfig, workflowID string) (*Worktree, error) {
	return CreateWorktreeWith(context.Background(), defaultExec, repoDir, cfg, workflowID)
}

// CreateWorktreeWith creates a worktree using the provided GitExec.
func CreateWorktreeWith(ctx context.Context, ge *GitExec, repoDir string, cfg WorktreeConfig, workflowID string) (*Worktree, error) {
	baseDir := cfg.BaseDir
	if baseDir == "" {
		baseDir = filepath.Join(repoDir, ".agents", "ycode", "worktrees")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create worktree base dir: %w", err)
	}

	branch := fmt.Sprintf("ycode/workflow/%s", workflowID)
	worktreePath := filepath.Join(baseDir, workflowID)

	// Create the worktree.
	args := []string{"worktree", "add", "-b", branch, worktreePath}
	if cfg.BaseBranch != "" {
		args = append(args, cfg.BaseBranch)
	}
	if _, err := ge.Run(ctx, repoDir, args...); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	return &Worktree{Path: worktreePath, Branch: branch}, nil
}

// CleanupWorktree removes a worktree and its branch.
func CleanupWorktree(repoDir string, wt *Worktree) error {
	return CleanupWorktreeWith(context.Background(), defaultExec, repoDir, wt)
}

// CleanupWorktreeWith removes a worktree using the provided GitExec.
func CleanupWorktreeWith(ctx context.Context, ge *GitExec, repoDir string, wt *Worktree) error {
	// Remove the worktree (ignore error if already removed).
	_, _ = ge.Run(ctx, repoDir, "worktree", "remove", wt.Path, "--force")

	// Delete the branch (ignore error if branch doesn't exist).
	_, _ = ge.Run(ctx, repoDir, "branch", "-D", wt.Branch)

	return nil
}

// ListWorktrees returns active worktrees.
func ListWorktrees(repoDir string) ([]Worktree, error) {
	return ListWorktreesWith(context.Background(), defaultExec, repoDir)
}

// ListWorktreesWith lists worktrees using the provided GitExec.
func ListWorktreesWith(ctx context.Context, ge *GitExec, repoDir string) ([]Worktree, error) {
	out, err := ge.Run(ctx, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	var current Worktree
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		} else if strings.HasPrefix(line, "branch ") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}
	return worktrees, nil
}
