package gitserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/git"
)

// WorkspaceMode determines how a workspace is mounted for an agent.
type WorkspaceMode int

const (
	// WorkspaceReadOnly mounts the workspace as read-only.
	// Suitable for Explore and Plan agents that don't modify files.
	WorkspaceReadOnly WorkspaceMode = iota

	// WorkspaceWorktree creates a git worktree on a unique branch.
	// Suitable for write agents that need isolated file modifications.
	WorkspaceWorktree
)

// WorkspaceInfo holds information about a prepared agent workspace.
type WorkspaceInfo struct {
	Mode     WorkspaceMode
	Path     string // host path to the workspace
	Branch   string // git branch name (only for WorkspaceWorktree)
	RepoDir  string // original repo directory
	ReadOnly bool   // mount as read-only
}

// ge is used for git operations in workspace management.
var ge = git.NewGitExec(nil)

// PrepareWorkspace sets up an isolated workspace for an agent.
func PrepareWorkspace(ctx context.Context, repoDir, agentID string, mode WorkspaceMode) (*WorkspaceInfo, error) {
	switch mode {
	case WorkspaceReadOnly:
		return &WorkspaceInfo{
			Mode:     WorkspaceReadOnly,
			Path:     repoDir,
			RepoDir:  repoDir,
			ReadOnly: true,
		}, nil

	case WorkspaceWorktree:
		branch := fmt.Sprintf("agent/%s", agentID)
		worktreePath := fmt.Sprintf("%s-worktree-%s", repoDir, agentID[:8])

		if err := createWorktree(ctx, repoDir, worktreePath, branch); err != nil {
			return nil, fmt.Errorf("create worktree for agent %s: %w", agentID, err)
		}

		slog.Info("gitserver: created worktree",
			"agent", agentID[:8],
			"branch", branch,
			"path", worktreePath,
		)

		return &WorkspaceInfo{
			Mode:     WorkspaceWorktree,
			Path:     worktreePath,
			Branch:   branch,
			RepoDir:  repoDir,
			ReadOnly: false,
		}, nil

	default:
		return nil, fmt.Errorf("unknown workspace mode: %d", mode)
	}
}

// CleanupWorkspace removes a workspace created by PrepareWorkspace.
func CleanupWorkspace(ctx context.Context, info *WorkspaceInfo) error {
	if info.Mode != WorkspaceWorktree {
		return nil // nothing to clean up for read-only
	}

	return removeWorktree(ctx, info.RepoDir, info.Path)
}

// MergeWorktree merges changes from an agent's worktree branch back to the base branch.
func MergeWorktree(ctx context.Context, info *WorkspaceInfo, baseBranch string) error {
	if info.Mode != WorkspaceWorktree || info.Branch == "" {
		return nil
	}

	// Check if the agent branch has any commits ahead of base.
	out, err := ge.Run(ctx, info.RepoDir, "log", "--oneline", baseBranch+".."+info.Branch)
	if err != nil || strings.TrimSpace(out) == "" {
		slog.Info("gitserver: no changes to merge", "branch", info.Branch)
		return nil
	}

	// Merge agent branch into base.
	mergeOut, err := ge.Run(ctx, info.RepoDir,
		"merge", "--no-ff", "-m",
		fmt.Sprintf("Merge agent branch %s", info.Branch),
		info.Branch)
	if err != nil {
		return fmt.Errorf("merge %s into %s: %w\n%s", info.Branch, baseBranch, err, mergeOut)
	}

	slog.Info("gitserver: merged agent branch", "branch", info.Branch, "into", baseBranch)
	return nil
}

// createWorktree creates a git worktree at the given path on a new branch.
func createWorktree(ctx context.Context, repoDir, worktreePath, branch string) error {
	_, err := ge.Run(ctx, repoDir, "worktree", "add", "-b", branch, worktreePath)
	if err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// removeWorktree removes a git worktree.
func removeWorktree(ctx context.Context, repoDir, worktreePath string) error {
	_, err := ge.Run(ctx, repoDir, "worktree", "remove", "--force", worktreePath)
	if err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}
