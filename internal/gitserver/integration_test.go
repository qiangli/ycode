//go:build integration

package gitserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	cmds := [][]string{
		{"git", "-C", repoDir, "init", "-b", "main"},
		{"git", "-C", repoDir, "config", "user.email", "test@test.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test Agent"},
	}

	testFile := filepath.Join(repoDir, "README.md")
	os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644)
	cmds = append(cmds,
		[]string{"git", "-C", repoDir, "add", "."},
		[]string{"git", "-C", repoDir, "commit", "-m", "initial commit"},
	)

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, string(out))
		}
	}

	return repoDir
}

func TestIntegrationWorkspaceWorktreeLifecycle(t *testing.T) {
	repoDir := setupTestRepo(t)
	ctx := context.Background()

	// Create worktree for agent.
	info, err := PrepareWorkspace(ctx, repoDir, "agent-int-test-001", WorkspaceWorktree)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}

	// Verify worktree exists.
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		t.Fatal("worktree path does not exist")
	}

	// Verify branch was created.
	if info.Branch != "agent/agent-int-test-001" {
		t.Errorf("unexpected branch: %s", info.Branch)
	}

	// Make a change in the worktree.
	changeFile := filepath.Join(info.Path, "agent-change.txt")
	os.WriteFile(changeFile, []byte("agent was here\n"), 0o644)

	cmd := exec.Command("git", "-C", info.Path, "add", "agent-change.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, string(out))
	}

	cmd = exec.Command("git", "-C", info.Path, "commit", "-m", "agent: add change")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Agent",
		"GIT_AUTHOR_EMAIL=agent@test.com",
		"GIT_COMMITTER_NAME=Agent",
		"GIT_COMMITTER_EMAIL=agent@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, string(out))
	}

	// Merge worktree back to main.
	// First checkout main in the original repo.
	cmd = exec.Command("git", "-C", repoDir, "checkout", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, string(out))
	}

	if err := MergeWorktree(ctx, info, "main"); err != nil {
		t.Fatalf("MergeWorktree: %v", err)
	}

	// Verify the change is in main.
	mergedFile := filepath.Join(repoDir, "agent-change.txt")
	if _, err := os.Stat(mergedFile); os.IsNotExist(err) {
		t.Error("merged file not found in main branch")
	}

	// Cleanup worktree.
	if err := CleanupWorkspace(ctx, info); err != nil {
		t.Fatalf("CleanupWorkspace: %v", err)
	}

	// Verify worktree was removed.
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Error("worktree path should be removed after cleanup")
	}
}

func TestIntegrationMultipleWorktrees(t *testing.T) {
	repoDir := setupTestRepo(t)
	ctx := context.Background()

	// Create two worktrees for different agents.
	info1, err := PrepareWorkspace(ctx, repoDir, "agent-alpha-001", WorkspaceWorktree)
	if err != nil {
		t.Fatalf("PrepareWorkspace agent1: %v", err)
	}
	defer CleanupWorkspace(ctx, info1)

	info2, err := PrepareWorkspace(ctx, repoDir, "agent-beta-002", WorkspaceWorktree)
	if err != nil {
		t.Fatalf("PrepareWorkspace agent2: %v", err)
	}
	defer CleanupWorkspace(ctx, info2)

	// Verify they have different paths and branches.
	if info1.Path == info2.Path {
		t.Error("worktrees should have different paths")
	}
	if info1.Branch == info2.Branch {
		t.Error("worktrees should have different branches")
	}

	// Make changes in each worktree.
	os.WriteFile(filepath.Join(info1.Path, "from-alpha.txt"), []byte("alpha\n"), 0o644)
	os.WriteFile(filepath.Join(info2.Path, "from-beta.txt"), []byte("beta\n"), 0o644)

	// Commit in each.
	for _, info := range []*WorkspaceInfo{info1, info2} {
		cmd := exec.Command("git", "-C", info.Path, "add", ".")
		cmd.CombinedOutput()
		cmd = exec.Command("git", "-C", info.Path, "commit", "-m", "agent change")
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Agent",
			"GIT_AUTHOR_EMAIL=agent@test.com",
			"GIT_COMMITTER_NAME=Agent",
			"GIT_COMMITTER_EMAIL=agent@test.com",
		)
		cmd.CombinedOutput()
	}

	// Both should exist independently.
	if _, err := os.Stat(filepath.Join(info1.Path, "from-alpha.txt")); err != nil {
		t.Error("alpha file missing")
	}
	if _, err := os.Stat(filepath.Join(info2.Path, "from-beta.txt")); err != nil {
		t.Error("beta file missing")
	}
}

func TestIntegrationMergeWorktreeNoChanges(t *testing.T) {
	repoDir := setupTestRepo(t)
	ctx := context.Background()

	info, err := PrepareWorkspace(ctx, repoDir, "agent-noop-001", WorkspaceWorktree)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	defer CleanupWorkspace(ctx, info)

	// Merge with no changes should be a no-op.
	cmd := exec.Command("git", "-C", repoDir, "checkout", "main")
	cmd.CombinedOutput()

	if err := MergeWorktree(ctx, info, "main"); err != nil {
		t.Fatalf("MergeWorktree (no changes): %v", err)
	}
}
