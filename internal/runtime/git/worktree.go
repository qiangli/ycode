package git

import (
	"fmt"
	"os"
	"os/exec"
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
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return &Worktree{Path: worktreePath, Branch: branch}, nil
}

// CleanupWorktree removes a worktree and its branch.
func CleanupWorktree(repoDir string, wt *Worktree) error {
	// Remove the worktree.
	cmd := exec.Command("git", "worktree", "remove", wt.Path, "--force")
	cmd.Dir = repoDir
	cmd.CombinedOutput() // ignore error if already removed

	// Delete the branch.
	cmd = exec.Command("git", "branch", "-D", wt.Branch)
	cmd.Dir = repoDir
	cmd.CombinedOutput() // ignore error if branch doesn't exist

	return nil
}

// ListWorktrees returns active worktrees.
func ListWorktrees(repoDir string) ([]Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	var current Worktree
	for _, line := range strings.Split(string(out), "\n") {
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
