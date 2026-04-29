package git

import (
	"context"
	"fmt"
	"strings"
)

// BranchLockCollision detects if another agent is working on the same branch.
type BranchLockCollision struct {
	Branch    string
	RemoteRef string
	Conflict  string
}

// DetectBranchCollision checks if the current branch has diverged from remote
// or if another process is working on it.
func DetectBranchCollision(dir string) (*BranchLockCollision, error) {
	return DetectBranchCollisionWith(context.Background(), dir, defaultExec)
}

// DetectBranchCollisionWith checks branch collision using the provided GitExec.
func DetectBranchCollisionWith(ctx context.Context, dir string, ge *GitExec) (*BranchLockCollision, error) {
	// Get current branch.
	branch, err := ge.RunOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("get current branch: %w", err)
	}

	// Check if tracking a remote.
	upstream, err := ge.RunOutput(ctx, dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return nil, nil // no upstream, no collision possible
	}

	// Fetch to ensure we have latest.
	_ = ge.RunCheck(ctx, dir, "fetch", "--quiet")

	// Check divergence.
	aheadBehind, err := ge.RunOutput(ctx, dir, "rev-list", "--left-right", "--count", branch+"..."+upstream)
	if err != nil {
		return nil, nil
	}

	parts := strings.Fields(aheadBehind)
	if len(parts) != 2 {
		return nil, nil
	}

	behind := parts[1]
	if behind != "0" {
		return &BranchLockCollision{
			Branch:    branch,
			RemoteRef: upstream,
			Conflict:  fmt.Sprintf("branch is %s commits behind %s", behind, upstream),
		}, nil
	}

	return nil, nil
}
