// Package loomlocal is the lightweight `local` backend mode from the
// loom v2 design (docs/loom-v2-plan.md "Backend modes"). It satisfies
// pkg/loom.Backend WITHOUT requiring an embedded Gitea or running
// merger goroutine — the convergence point is a local bare repo plus
// `git rebase main && git merge --ff-only` against it.
//
// Use this mode for small projects with no CI worth gating on, where
// the forge round-trip cost outweighs its semantic benefits.
//
// Implementation status (N+2 N2.3 — optional): scaffold only. The
// shape compiles and registers with the substrate. The actual
// merge / rebase / PR-state logic lands in a focused follow-up PR
// once a production user requests the mode in earnest. Until then,
// pkg/loom.Backend methods return ErrLocalNotYetImplemented so a
// misconfigured operator gets a clear error rather than silent
// fall-through behavior.
//
// The forge backend remains the default (docs/loom-v2-plan.md "Backend
// modes — Auto-detection"); operators must explicitly opt into local
// via backend_mode: local in .ycode/loom.yaml.
package loomlocal

import (
	"context"
	"errors"
	"fmt"

	"github.com/qiangli/ycode/pkg/loom"
)

// ErrLocalNotYetImplemented is returned by every Backend method on
// the local backend until the production logic lands. Surfaces as a
// configuration error rather than a panic so operators can react.
var ErrLocalNotYetImplemented = errors.New("loomlocal: backend not yet implemented (N+2 N2.3 ships the seam; merger/rebase logic lands in a follow-up PR)")

// Backend implements pkg/loom.Backend against a local bare repo
// instead of an embedded Gitea. The convergence flow rebases each
// lease's branch onto the bare's main and ff-merges; no PRs, no CI,
// no merger goroutine.
type Backend struct {
	BareRepoPath string
}

// New constructs a local Backend pointed at the given bare repo.
func New(bareRepoPath string) (*Backend, error) {
	if bareRepoPath == "" {
		return nil, errors.New("loomlocal: BareRepoPath required")
	}
	return &Backend{BareRepoPath: bareRepoPath}, nil
}

// Compile-time check that Backend satisfies the interface even while
// the methods return the not-yet-implemented sentinel.
var _ loom.Backend = (*Backend)(nil)

func (b *Backend) EnsureProject(ctx context.Context, cwd string) (string, string, error) {
	return "", "", fmt.Errorf("EnsureProject: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) PrepareSandbox(ctx context.Context, sandboxRoot, slug, branch, agentID, name, email, cloneURL string) (string, error) {
	return "", fmt.Errorf("PrepareSandbox: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) CommitAndPush(ctx context.Context, sandboxPath, slug, branch, message string, force bool) (string, error) {
	return "", fmt.Errorf("CommitAndPush: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) EnsureRemoteBranch(ctx context.Context, slug, branch string) error {
	return fmt.Errorf("EnsureRemoteBranch: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) OpenPR(ctx context.Context, slug, branch, title, body string) (int64, error) {
	return 0, fmt.Errorf("OpenPR: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) ListPRStates(ctx context.Context, slug, branchPrefix string) ([]loom.BackendPRState, error) {
	return nil, fmt.Errorf("ListPRStates: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) DeleteSandbox(path string) error {
	return fmt.Errorf("DeleteSandbox: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) DeleteBranch(ctx context.Context, slug, branch string) error {
	return fmt.Errorf("DeleteBranch: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) NotifyProjectActive(ctx context.Context, slug, cloneURL string) error {
	return fmt.Errorf("NotifyProjectActive: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) ClaimNextIssue(ctx context.Context, slug string) (int64, error) {
	return 0, fmt.Errorf("ClaimNextIssue: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) Checkpoint(ctx context.Context, sandboxPath, message string) (string, error) {
	return "", fmt.Errorf("Checkpoint: %w", ErrLocalNotYetImplemented)
}

func (b *Backend) RebaseSandbox(ctx context.Context, sandboxPath, baseBranch string) ([]string, error) {
	return nil, fmt.Errorf("RebaseSandbox: %w", ErrLocalNotYetImplemented)
}
