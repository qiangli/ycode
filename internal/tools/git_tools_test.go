package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupGitRepo creates a temporary git repo for testing.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	run("init")
	run("checkout", "-b", "main")

	// Create initial commit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")

	return dir
}

func TestGitStatusHandler(t *testing.T) {
	dir := setupGitRepo(t)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterGitHandlers(r, dir)

	spec, ok := r.Get("git_status")
	if !ok {
		t.Fatal("git_status not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	// Clean repo should show branch info.
	if result == "" {
		t.Error("expected non-empty status output")
	}
}

func TestGitLogHandler(t *testing.T) {
	dir := setupGitRepo(t)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterGitHandlers(r, dir)

	spec, ok := r.Get("git_log")
	if !ok {
		t.Fatal("git_log not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{"count": 5}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == "" || result == "(no commits)" {
		t.Error("expected commit history")
	}
}

func TestGitCommitHandler(t *testing.T) {
	dir := setupGitRepo(t)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterGitHandlers(r, dir)

	// Create a new file to commit.
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, ok := r.Get("git_commit")
	if !ok {
		t.Fatal("git_commit not registered")
	}

	input := `{"message": "add test file", "files": ["test.txt"]}`
	result, err := spec.Handler(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Error("expected commit output")
	}
}

func TestGitBranchHandler(t *testing.T) {
	dir := setupGitRepo(t)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterGitHandlers(r, dir)

	spec, ok := r.Get("git_branch")
	if !ok {
		t.Fatal("git_branch not registered")
	}

	// List branches.
	result, err := spec.Handler(context.Background(), json.RawMessage(`{"action": "list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Error("expected branch list")
	}

	// Create branch.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"action": "create", "name": "feature-1"}`))
	if err != nil {
		t.Fatal(err)
	}

	// Switch branch.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"action": "switch", "name": "feature-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = result
}

func TestGitStashHandler(t *testing.T) {
	dir := setupGitRepo(t)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterGitHandlers(r, dir)

	// Create a modification to stash.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, ok := r.Get("git_stash")
	if !ok {
		t.Fatal("git_stash not registered")
	}

	// Push to stash.
	result, err := spec.Handler(context.Background(), json.RawMessage(`{"action": "push", "message": "test stash"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Error("expected stash output")
	}

	// List stash.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"action": "list"}`))
	if err != nil {
		t.Fatal(err)
	}

	// Pop stash.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"action": "pop"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = result
}

func TestTruncateOutput(t *testing.T) {
	short := "hello"
	if truncateOutput(short) != short {
		t.Error("should not truncate short strings")
	}

	long := string(make([]byte, maxGitOutput+100))
	result := truncateOutput(long)
	if len(result) > maxGitOutput+50 {
		t.Error("should truncate long strings")
	}
}
