package gitserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPrepareWorkspaceReadOnly(t *testing.T) {
	dir := t.TempDir()
	info, err := PrepareWorkspace(context.Background(), dir, "agent-001", WorkspaceReadOnly)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if info.Mode != WorkspaceReadOnly {
		t.Error("expected read-only mode")
	}
	if !info.ReadOnly {
		t.Error("expected ReadOnly flag")
	}
	if info.Path != dir {
		t.Errorf("expected path %s, got %s", dir, info.Path)
	}
}

func TestPrepareWorkspaceWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping worktree test in short mode")
	}

	// Check git is available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a temporary git repo.
	repoDir := t.TempDir()
	cmds := [][]string{
		{"git", "-C", repoDir, "init"},
		{"git", "-C", repoDir, "config", "user.email", "test@test.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test"},
	}

	// Create initial commit.
	testFile := filepath.Join(repoDir, "README.md")
	os.WriteFile(testFile, []byte("# Test\n"), 0o644)
	cmds = append(cmds,
		[]string{"git", "-C", repoDir, "add", "."},
		[]string{"git", "-C", repoDir, "commit", "-m", "initial"},
	)

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, string(out))
		}
	}

	ctx := context.Background()
	info, err := PrepareWorkspace(ctx, repoDir, "agent-test-1234", WorkspaceWorktree)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}

	if info.Mode != WorkspaceWorktree {
		t.Error("expected worktree mode")
	}
	if info.ReadOnly {
		t.Error("worktree should not be read-only")
	}
	if info.Branch != "agent/agent-test-1234" {
		t.Errorf("unexpected branch: %s", info.Branch)
	}

	// Verify the worktree directory exists.
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		t.Error("worktree path does not exist")
	}

	// Clean up.
	if err := CleanupWorkspace(ctx, info); err != nil {
		t.Fatalf("CleanupWorkspace: %v", err)
	}

	// Verify the worktree was removed.
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Error("worktree path still exists after cleanup")
	}
}

func TestCleanupWorkspaceReadOnly(t *testing.T) {
	info := &WorkspaceInfo{Mode: WorkspaceReadOnly}
	if err := CleanupWorkspace(context.Background(), info); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
