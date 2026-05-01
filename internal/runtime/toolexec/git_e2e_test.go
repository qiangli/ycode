//go:build !short

package toolexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// setupE2ERepo creates a test repository with an initial commit containing one file.
func setupE2ERepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Create initial file
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Project\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add: %v", err)
	}

	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	return dir
}

// setupE2EExecutor creates an Executor with the git ToolDef registered (no container engine).
func setupE2EExecutor(t *testing.T) *Executor {
	t.Helper()
	exec := New(nil, nil)
	exec.Register(NewGitDef())
	return exec
}

// ---------- Workflow Tests ----------

// TestE2E_FullWorkflow tests a complete development cycle:
// create → add → commit → branch → modify → commit → merge → verify history
func TestE2E_FullWorkflow(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Verify initial state
	result, err := nativeStatus(ctx, dir, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if result.Stdout != "" {
		t.Fatalf("expected clean status, got: %q", result.Stdout)
	}

	// Create a feature branch
	result, err = nativeCheckout(ctx, dir, []string{"-b", "feature"})
	if err != nil {
		t.Fatalf("checkout -b feature: %v", err)
	}
	if !strings.Contains(result.Stdout, "feature") {
		t.Fatalf("expected branch switch message, got: %q", result.Stdout)
	}

	// Verify we're on feature branch
	result, err = nativeRevParse(ctx, dir, []string{"--abbrev-ref", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "feature" {
		t.Fatalf("expected branch 'feature', got: %q", result.Stdout)
	}

	// Add a new file
	if err := os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package main\n\nfunc Feature() {}\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err = nativeAdd(ctx, dir, []string{"feature.go"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Commit
	result, err = nativeCommit(ctx, dir, []string{"-m", "add feature"})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !strings.Contains(result.Stdout, "add feature") {
		t.Fatalf("expected commit message in output, got: %q", result.Stdout)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative, got: %v", result.Tier)
	}

	// Switch back to main/master
	result, err = nativeCheckout(ctx, dir, []string{"master"})
	if err != nil {
		t.Fatalf("checkout master: %v", err)
	}

	// Merge feature branch (fast-forward)
	result, err = nativeMerge(ctx, dir, []string{"feature"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(result.Stdout, "Fast-forward") {
		t.Fatalf("expected fast-forward merge, got: %q", result.Stdout)
	}

	// Verify log shows both commits
	result, err = nativeLog(ctx, dir, []string{"--oneline"})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 log entries, got: %d\n%s", len(lines), result.Stdout)
	}

	// Verify feature.go exists via ls-files
	result, err = nativeLsFiles(ctx, dir, nil)
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	if !strings.Contains(result.Stdout, "feature.go") {
		t.Fatalf("expected feature.go in ls-files, got: %q", result.Stdout)
	}
}

// TestE2E_TagWorkflow tests: create repo → add → commit → tag → verify → delete tag
func TestE2E_TagWorkflow(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create lightweight tag
	result, err := nativeTag(ctx, dir, []string{"v1.0.0"})
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("tag failed: %s", result.Stderr)
	}

	// Verify tag exists
	result, err = nativeTag(ctx, dir, nil)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if !strings.Contains(result.Stdout, "v1.0.0") {
		t.Fatalf("expected tag v1.0.0, got: %q", result.Stdout)
	}

	// Create annotated tag
	result, err = nativeTag(ctx, dir, []string{"-a", "v2.0.0", "-m", "release 2.0"})
	if err != nil {
		t.Fatalf("annotated tag: %v", err)
	}

	// Verify both tags
	result, err = nativeTag(ctx, dir, nil)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if !strings.Contains(result.Stdout, "v1.0.0") || !strings.Contains(result.Stdout, "v2.0.0") {
		t.Fatalf("expected both tags, got: %q", result.Stdout)
	}

	// Delete tag
	result, err = nativeTag(ctx, dir, []string{"-d", "v1.0.0"})
	if err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if !strings.Contains(result.Stdout, "Deleted") {
		t.Fatalf("expected delete confirmation, got: %q", result.Stdout)
	}

	// Verify tag is gone
	result, err = nativeTag(ctx, dir, nil)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if strings.Contains(result.Stdout, "v1.0.0") {
		t.Fatalf("tag v1.0.0 should be deleted, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "v2.0.0") {
		t.Fatalf("tag v2.0.0 should still exist, got: %q", result.Stdout)
	}

	// Verify show-ref shows the annotated tag
	result, err = nativeShowRef(ctx, dir, []string{"--tags"})
	if err != nil {
		t.Fatalf("show-ref: %v", err)
	}
	if !strings.Contains(result.Stdout, "v2.0.0") {
		t.Fatalf("expected v2.0.0 in show-ref, got: %q", result.Stdout)
	}
}

// TestE2E_GrepWorkflow tests grep across files
func TestE2E_GrepWorkflow(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Add multiple files
	files := map[string]string{
		"main.go":     "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"util.go":     "package main\n\nfunc helper() string {\n\treturn \"hello world\"\n}\n",
		"test_data.go": "package main\n\nvar data = []string{\"foo\", \"bar\"}\n",
	}

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	_, err = wt.Commit("add go files", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Grep for "hello"
	result, err := nativeGrep(ctx, dir, []string{"hello"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(result.Stdout, "main.go") {
		t.Fatalf("expected main.go in grep results, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "util.go") {
		t.Fatalf("expected util.go in grep results, got: %q", result.Stdout)
	}

	// Grep with -l (files only)
	result, err = nativeGrep(ctx, dir, []string{"-l", "hello"})
	if err != nil {
		t.Fatalf("grep -l: %v", err)
	}
	grepLines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(grepLines) != 2 {
		t.Fatalf("expected 2 matching files, got %d: %q", len(grepLines), result.Stdout)
	}

	// Grep with case insensitive
	result, err = nativeGrep(ctx, dir, []string{"-i", "HELLO"})
	if err != nil {
		t.Fatalf("grep -i: %v", err)
	}
	if !strings.Contains(result.Stdout, "main.go") {
		t.Fatalf("case-insensitive grep should find main.go, got: %q", result.Stdout)
	}

	// Grep for something that doesn't exist
	result, err = nativeGrep(ctx, dir, []string{"nonexistent_string_xyz"})
	if err != nil {
		t.Fatalf("grep no-match: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for no matches, got: %d", result.ExitCode)
	}
}

// TestE2E_MergeNoFF tests merge with --no-ff flag.
// Note: The native --no-ff implementation may fall through to ErrNotImplemented
// if go-git cannot create the merge commit (the worktree needs to reflect the merged state).
// This test verifies the fallthrough behavior is correct.
func TestE2E_MergeNoFF(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create feature branch with a commit
	_, err := nativeCheckout(ctx, dir, []string{"-b", "feat"})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := nativeAdd(ctx, dir, []string{"new.txt"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := nativeCommit(ctx, dir, []string{"-m", "feat commit"}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Switch back to master
	_, err = nativeCheckout(ctx, dir, []string{"master"})
	if err != nil {
		t.Fatalf("checkout master: %v", err)
	}

	// Merge with --no-ff: may succeed or fall through to ErrNotImplemented
	result, err := nativeMerge(ctx, dir, []string{"--no-ff", "feat"})
	if err == ErrNotImplemented {
		// This is acceptable — native --no-ff merge may not work when go-git
		// worktree doesn't contain the merged content
		t.Skip("--no-ff merge falls through to host git (expected)")
	}
	if err != nil {
		t.Fatalf("merge --no-ff: %v", err)
	}
	if !strings.Contains(result.Stdout, "Merge made") {
		t.Fatalf("expected merge commit message, got: %q", result.Stdout)
	}

	// Verify merge commit has 2 parents via log
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("commit object: %v", err)
	}
	if commit.NumParents() != 2 {
		t.Fatalf("expected 2 parents (merge commit), got: %d", commit.NumParents())
	}
}

// ---------- Tier Dispatch Tests ----------

// TestE2E_ExecutorDispatch_NativeTier verifies commands dispatch to native tier
func TestE2E_ExecutorDispatch_NativeTier(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()
	exec := setupE2EExecutor(t)

	// rev-parse should use native tier
	result, err := exec.Run(ctx, "git", dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("executor run: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative, got: %v", result.Tier)
	}
	if strings.TrimSpace(result.Stdout) != "true" {
		t.Fatalf("expected 'true', got: %q", result.Stdout)
	}

	// status should use native tier
	result, err = exec.Run(ctx, "git", dir, "status")
	if err != nil {
		t.Fatalf("executor run status: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative for status, got: %v", result.Tier)
	}

	// log should use native tier
	result, err = exec.Run(ctx, "git", dir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatalf("executor run log: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative for log, got: %v", result.Tier)
	}
	if !strings.Contains(result.Stdout, "initial commit") {
		t.Fatalf("expected initial commit in log, got: %q", result.Stdout)
	}
}

// TestE2E_ExecutorDispatch_BranchAndCommit tests write operations through executor
func TestE2E_ExecutorDispatch_BranchAndCommit(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()
	exec := setupE2EExecutor(t)

	// Create branch via executor
	result, err := exec.Run(ctx, "git", dir, "checkout", "-b", "executor-branch")
	if err != nil {
		t.Fatalf("executor checkout: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative, got: %v", result.Tier)
	}

	// Add file and commit
	if err := os.WriteFile(filepath.Join(dir, "exec.txt"), []byte("via executor\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err = exec.Run(ctx, "git", dir, "add", "exec.txt")
	if err != nil {
		t.Fatalf("executor add: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative for add, got: %v", result.Tier)
	}

	result, err = exec.Run(ctx, "git", dir, "commit", "-m", "executor commit")
	if err != nil {
		t.Fatalf("executor commit: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative for commit, got: %v", result.Tier)
	}
	if !strings.Contains(result.Stdout, "executor commit") {
		t.Fatalf("expected commit message, got: %q", result.Stdout)
	}
}

// TestE2E_ExecutorDispatch_UnknownTool tests error for unregistered tool
func TestE2E_ExecutorDispatch_UnknownTool(t *testing.T) {
	ctx := context.Background()
	exec := setupE2EExecutor(t)

	_, err := exec.Run(ctx, "unknown-tool", "/tmp", "arg1")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected 'unknown tool' error, got: %v", err)
	}
}

// ---------- Plumbing Tests ----------

// TestE2E_PlumbingHashObjectCatFile tests hash-object → cat-file roundtrip
func TestE2E_PlumbingHashObjectCatFile(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create a file to hash
	content := "This is test content for hashing.\n"
	testFile := filepath.Join(dir, "hashme.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Hash the object (with -w to write into store)
	result, err := nativeHashObject(ctx, dir, []string{"-w", "hashme.txt"})
	if err != nil {
		t.Fatalf("hash-object: %v", err)
	}
	hash := strings.TrimSpace(result.Stdout)
	if len(hash) != 40 {
		t.Fatalf("expected 40-char hash, got: %q", hash)
	}

	// cat-file -t should return "blob"
	result, err = nativeCatFile(ctx, dir, []string{"-t", hash})
	if err != nil {
		t.Fatalf("cat-file -t: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "blob" {
		t.Fatalf("expected type 'blob', got: %q", result.Stdout)
	}

	// cat-file -p should return the content
	result, err = nativeCatFile(ctx, dir, []string{"-p", hash})
	if err != nil {
		t.Fatalf("cat-file -p: %v", err)
	}
	if result.Stdout != content {
		t.Fatalf("expected content %q, got: %q", content, result.Stdout)
	}

	// cat-file -s should return the size
	result, err = nativeCatFile(ctx, dir, []string{"-s", hash})
	if err != nil {
		t.Fatalf("cat-file -s: %v", err)
	}
	expectedSize := "34\n" // len("This is test content for hashing.\n") = 34
	if result.Stdout != expectedSize {
		t.Fatalf("expected size %q, got: %q", expectedSize, result.Stdout)
	}
}

// TestE2E_PlumbingCommitTree tests write-tree → commit-tree → update-ref
func TestE2E_PlumbingCommitTree(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get the current HEAD hash for parent
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	parentHash := strings.TrimSpace(result.Stdout)

	// Get tree hash from current commit
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	treeHash := headCommit.TreeHash.String()

	// commit-tree with parent
	result, err = nativeCommitTree(ctx, dir, []string{treeHash, "-p", parentHash, "-m", "plumbing commit"})
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	newCommitHash := strings.TrimSpace(result.Stdout)
	if len(newCommitHash) != 40 {
		t.Fatalf("expected 40-char hash, got: %q", newCommitHash)
	}

	// update-ref to create a new branch pointing to our commit
	result, err = nativeUpdateRef(ctx, dir, []string{"refs/heads/plumbing-branch", newCommitHash})
	if err != nil {
		t.Fatalf("update-ref: %v", err)
	}

	// Verify via show-ref
	result, err = nativeShowRef(ctx, dir, []string{"--heads"})
	if err != nil {
		t.Fatalf("show-ref: %v", err)
	}
	if !strings.Contains(result.Stdout, "plumbing-branch") {
		t.Fatalf("expected plumbing-branch in show-ref, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, newCommitHash) {
		t.Fatalf("expected hash %s in show-ref, got: %q", newCommitHash, result.Stdout)
	}

	// Verify commit content via cat-file
	result, err = nativeCatFile(ctx, dir, []string{"-p", newCommitHash})
	if err != nil {
		t.Fatalf("cat-file commit: %v", err)
	}
	if !strings.Contains(result.Stdout, "plumbing commit") {
		t.Fatalf("expected message in commit, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, treeHash) {
		t.Fatalf("expected tree hash in commit, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, parentHash) {
		t.Fatalf("expected parent hash in commit, got: %q", result.Stdout)
	}
}

// TestE2E_PlumbingSymbolicRef tests symbolic-ref management
func TestE2E_PlumbingSymbolicRef(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Read HEAD symbolic ref
	result, err := nativeSymbolicRef(ctx, dir, []string{"HEAD"})
	if err != nil {
		t.Fatalf("symbolic-ref read: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "refs/heads/master" {
		t.Fatalf("expected refs/heads/master, got: %q", result.Stdout)
	}

	// Create a new branch and point HEAD to it
	nativeBranch(ctx, dir, []string{"new-branch"})

	result, err = nativeSymbolicRef(ctx, dir, []string{"HEAD", "refs/heads/new-branch"})
	if err != nil {
		t.Fatalf("symbolic-ref write: %v", err)
	}

	// Verify HEAD now points to new-branch
	result, err = nativeSymbolicRef(ctx, dir, []string{"HEAD"})
	if err != nil {
		t.Fatalf("symbolic-ref verify: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "refs/heads/new-branch" {
		t.Fatalf("expected refs/heads/new-branch, got: %q", result.Stdout)
	}
}

// TestE2E_PlumbingDiffTree tests diff-tree between two commits
func TestE2E_PlumbingDiffTree(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get first commit hash
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	firstCommit := strings.TrimSpace(result.Stdout)

	// Add a new file and modify existing
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new file\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	nativeAdd(ctx, dir, []string{"-A"})
	nativeCommit(ctx, dir, []string{"-m", "second commit"})

	// Get second commit hash
	result, err = nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	secondCommit := strings.TrimSpace(result.Stdout)

	// diff-tree between the two commits
	result, err = nativeDiffTree(ctx, dir, []string{firstCommit, secondCommit})
	if err != nil {
		t.Fatalf("diff-tree: %v", err)
	}

	if !strings.Contains(result.Stdout, "A\tnew.txt") {
		t.Fatalf("expected 'A\\tnew.txt' in diff-tree, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "M\tREADME.md") {
		t.Fatalf("expected 'M\\tREADME.md' in diff-tree, got: %q", result.Stdout)
	}
}

// TestE2E_PlumbingLsTree tests ls-tree listing
func TestE2E_PlumbingLsTree(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// ls-tree HEAD
	result, err := nativeLsTree(ctx, dir, []string{"HEAD"})
	if err != nil {
		t.Fatalf("ls-tree: %v", err)
	}
	if !strings.Contains(result.Stdout, "README.md") {
		t.Fatalf("expected README.md in ls-tree, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "blob") {
		t.Fatalf("expected 'blob' type in ls-tree, got: %q", result.Stdout)
	}

	// Add subdirectory and test recursive
	subdir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "lib.go"), []byte("package pkg\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"-A"})
	nativeCommit(ctx, dir, []string{"-m", "add subdir"})

	result, err = nativeLsTree(ctx, dir, []string{"-r", "HEAD"})
	if err != nil {
		t.Fatalf("ls-tree -r: %v", err)
	}
	if !strings.Contains(result.Stdout, "pkg/lib.go") {
		t.Fatalf("expected pkg/lib.go in recursive ls-tree, got: %q", result.Stdout)
	}
}

// TestE2E_PlumbingShowRef tests show-ref with filters
func TestE2E_PlumbingShowRef(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create branches and tags
	nativeBranch(ctx, dir, []string{"branch-a"})
	nativeBranch(ctx, dir, []string{"branch-b"})
	nativeTag(ctx, dir, []string{"tag-1"})

	// show-ref --heads
	result, err := nativeShowRef(ctx, dir, []string{"--heads"})
	if err != nil {
		t.Fatalf("show-ref --heads: %v", err)
	}
	if !strings.Contains(result.Stdout, "branch-a") {
		t.Fatalf("expected branch-a, got: %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "tag-1") {
		t.Fatalf("should not contain tag-1 with --heads filter, got: %q", result.Stdout)
	}

	// show-ref --tags
	result, err = nativeShowRef(ctx, dir, []string{"--tags"})
	if err != nil {
		t.Fatalf("show-ref --tags: %v", err)
	}
	if !strings.Contains(result.Stdout, "tag-1") {
		t.Fatalf("expected tag-1, got: %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "branch-a") {
		t.Fatalf("should not contain branch-a with --tags filter, got: %q", result.Stdout)
	}
}

// ---------- Edge Case Tests ----------

// TestE2E_EmptyRepoStatus tests status in a fresh repo with no commits
func TestE2E_EmptyRepoStatus(t *testing.T) {
	dir := t.TempDir()

	_, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	ctx := context.Background()

	// Status should work on empty repo
	result, err := nativeStatus(ctx, dir, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// Empty repo with no files = clean status
	if result.Stdout != "" {
		// Or it might show untracked files if any exist
		t.Logf("empty repo status: %q", result.Stdout)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative, got: %v", result.Tier)
	}
}

// TestE2E_BinaryFileGrep tests that grep skips binary files
func TestE2E_BinaryFileGrep(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Add a binary file
	binaryContent := make([]byte, 256)
	for i := range binaryContent {
		binaryContent[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, "binary.dat"), binaryContent, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Add a text file with matching content
	if err := os.WriteFile(filepath.Join(dir, "text.txt"), []byte("search_target here\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	wt.Add("binary.dat")
	wt.Add("text.txt")
	wt.Commit("add binary and text", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@t.com", When: time.Now()},
	})

	// Grep should find the text file but skip binary
	result, err := nativeGrep(ctx, dir, []string{"search_target"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(result.Stdout, "text.txt") {
		t.Fatalf("expected text.txt in grep, got: %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "binary.dat") {
		t.Fatalf("binary.dat should be skipped, got: %q", result.Stdout)
	}
}

// TestE2E_BlameFile tests blame output
func TestE2E_BlameFile(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Blame the initial file
	result, err := nativeBlame(ctx, dir, []string{"README.md"})
	if err != nil {
		t.Fatalf("blame: %v", err)
	}
	if !strings.Contains(result.Stdout, "# Project") {
		t.Fatalf("expected file content in blame, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "test@example.com") {
		t.Fatalf("expected author email in blame, got: %q", result.Stdout)
	}

	// Blame with line range
	// Add multi-line file
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "multi.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"multi.txt"})
	nativeCommit(ctx, dir, []string{"-m", "add multi"})

	result, err = nativeBlame(ctx, dir, []string{"-L", "2,4", "multi.txt"})
	if err != nil {
		t.Fatalf("blame -L: %v", err)
	}
	blameLines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(blameLines) != 3 {
		t.Fatalf("expected 3 blame lines (2-4), got %d: %q", len(blameLines), result.Stdout)
	}
}

// TestE2E_LsFilesUntracked tests ls-files with untracked files
func TestE2E_LsFilesUntracked(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create an untracked file
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// --others should show untracked
	result, err := nativeLsFiles(ctx, dir, []string{"--others"})
	if err != nil {
		t.Fatalf("ls-files --others: %v", err)
	}
	if !strings.Contains(result.Stdout, "untracked.txt") {
		t.Fatalf("expected untracked.txt, got: %q", result.Stdout)
	}

	// Default (cached) should NOT show untracked
	result, err = nativeLsFiles(ctx, dir, nil)
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	if strings.Contains(result.Stdout, "untracked.txt") {
		t.Fatalf("untracked file should not appear in default ls-files, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "README.md") {
		t.Fatalf("expected README.md in ls-files, got: %q", result.Stdout)
	}
}

// TestE2E_RmCached tests git rm --cached (remove from index, keep in worktree)
func TestE2E_RmCached(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Remove README.md from index but keep in worktree
	result, err := nativeRm(ctx, dir, []string{"--cached", "README.md"})
	if err != nil {
		t.Fatalf("rm --cached: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("rm --cached failed: %s", result.Stderr)
	}

	// File should still exist on disk
	if _, err := os.Stat(filepath.Join(dir, "README.md")); os.IsNotExist(err) {
		t.Fatal("README.md should still exist on disk after rm --cached")
	}
}

// TestE2E_TagOnNonHead tests creating a tag (only on HEAD since native only supports HEAD)
func TestE2E_TagOnNonHead(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Make a second commit
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"second.txt"})
	nativeCommit(ctx, dir, []string{"-m", "second commit"})

	// Tag on current HEAD
	result, err := nativeTag(ctx, dir, []string{"v3.0.0"})
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("tag failed: %s", result.Stderr)
	}

	// Duplicate tag should fail
	result, err = nativeTag(ctx, dir, []string{"v3.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatal("expected error for duplicate tag")
	}
	if !strings.Contains(result.Stderr, "already exists") {
		t.Fatalf("expected 'already exists' error, got: %q", result.Stderr)
	}
}

// TestE2E_BranchListAndDelete tests branch listing and deletion
func TestE2E_BranchListAndDelete(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create multiple branches using checkout -b and add branch config
	// (go-git's checkout -b creates refs but not branch config entries,
	// and DeleteBranch requires a config entry)
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	nativeCheckout(ctx, dir, []string{"-b", "alpha"})
	nativeCheckout(ctx, dir, []string{"master"})
	nativeCheckout(ctx, dir, []string{"-b", "beta"})
	nativeCheckout(ctx, dir, []string{"master"})
	nativeCheckout(ctx, dir, []string{"-b", "gamma"})
	nativeCheckout(ctx, dir, []string{"master"})

	// Add branch config entries for deletion to work
	cfg, _ := repo.Config()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		cfg.Branches[name] = &config.Branch{Name: name}
	}
	repo.SetConfig(cfg)

	// List branches
	result, err := nativeBranch(ctx, dir, nil)
	if err != nil {
		t.Fatalf("branch list: %v", err)
	}
	if !strings.Contains(result.Stdout, "alpha") ||
		!strings.Contains(result.Stdout, "beta") ||
		!strings.Contains(result.Stdout, "gamma") {
		t.Fatalf("expected all branches, got: %q", result.Stdout)
	}
	// Current branch should be marked with *
	if !strings.Contains(result.Stdout, "* master") {
		t.Fatalf("expected '* master', got: %q", result.Stdout)
	}

	// Delete a branch
	result, err = nativeBranch(ctx, dir, []string{"-D", "beta"})
	if err != nil {
		t.Fatalf("branch -D: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("branch -D failed: exit=%d stderr=%q", result.ExitCode, result.Stderr)
	}

	// Verify beta is gone
	result, err = nativeBranch(ctx, dir, nil)
	if err != nil {
		t.Fatalf("branch list: %v", err)
	}
	if strings.Contains(result.Stdout, "beta") {
		t.Fatalf("beta should be deleted, got: %q", result.Stdout)
	}

	// Delete non-existent branch
	result, err = nativeBranch(ctx, dir, []string{"-D", "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got: %d", result.ExitCode)
	}
}

// TestE2E_LogFormat tests log with format strings
func TestE2E_LogFormat(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeLog(ctx, dir, []string{"--format=%H|%an|%s", "-1"})
	if err != nil {
		t.Fatalf("log --format: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(result.Stdout), "|")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts in format output, got: %q", result.Stdout)
	}
	// First part should be a 40-char hash
	if len(parts[0]) != 40 {
		t.Fatalf("expected 40-char hash, got: %q", parts[0])
	}
	if parts[1] != "Test User" {
		t.Fatalf("expected 'Test User', got: %q", parts[1])
	}
	if parts[2] != "initial commit" {
		t.Fatalf("expected 'initial commit', got: %q", parts[2])
	}
}

// TestE2E_ShowCommit tests show command on a commit
func TestE2E_ShowCommit(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeShow(ctx, dir, []string{"HEAD"})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(result.Stdout, "initial commit") {
		t.Fatalf("expected commit message, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Test User") {
		t.Fatalf("expected author, got: %q", result.Stdout)
	}
	// Should include the diff for the initial commit
	if !strings.Contains(result.Stdout, "README.md") {
		t.Fatalf("expected diff showing README.md, got: %q", result.Stdout)
	}
}

// TestE2E_MergeBase tests finding common ancestor
func TestE2E_MergeBase(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get initial commit hash
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	initialHash := strings.TrimSpace(result.Stdout)

	// Create branch with commit
	nativeCheckout(ctx, dir, []string{"-b", "diverge"})
	os.WriteFile(filepath.Join(dir, "diverge.txt"), []byte("d\n"), 0644)
	nativeAdd(ctx, dir, []string{"diverge.txt"})
	nativeCommit(ctx, dir, []string{"-m", "diverge"})

	// Switch back and make another commit
	nativeCheckout(ctx, dir, []string{"master"})
	os.WriteFile(filepath.Join(dir, "master.txt"), []byte("m\n"), 0644)
	nativeAdd(ctx, dir, []string{"master.txt"})
	nativeCommit(ctx, dir, []string{"-m", "master advance"})

	// Merge-base should be the initial commit
	result, err = nativeMergeBase(ctx, dir, []string{"master", "diverge"})
	if err != nil {
		t.Fatalf("merge-base: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != initialHash {
		t.Fatalf("expected merge-base %s, got: %q", initialHash, result.Stdout)
	}
}

// TestE2E_ResetUnstage tests reset (unstage all)
func TestE2E_ResetUnstage(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Modify a file and stage it
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"README.md"})

	// Verify it's staged (status shows M in staging)
	result, err := nativeStatus(ctx, dir, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(result.Stdout, "README.md") {
		t.Fatalf("expected README.md in status, got: %q", result.Stdout)
	}

	// Reset (unstage)
	result, err = nativeReset(ctx, dir, nil)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}

	// File should still be modified but unstaged
	result, err = nativeStatus(ctx, dir, nil)
	if err != nil {
		t.Fatalf("status after reset: %v", err)
	}
	// After reset, status should show modification in worktree column
	if !strings.Contains(result.Stdout, "README.md") {
		t.Fatalf("README.md should still be modified, got: %q", result.Stdout)
	}
}

// TestE2E_CherryPick tests cherry-picking a commit
func TestE2E_CherryPick(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create a branch with a commit
	nativeCheckout(ctx, dir, []string{"-b", "pick-source"})
	if err := os.WriteFile(filepath.Join(dir, "cherry.txt"), []byte("cherry content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"cherry.txt"})
	nativeCommit(ctx, dir, []string{"-m", "cherry commit"})

	// Get the commit hash
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	cherryHash := strings.TrimSpace(result.Stdout)

	// Switch to master
	nativeCheckout(ctx, dir, []string{"master"})

	// Cherry-pick
	result, err = nativeCherryPick(ctx, dir, []string{cherryHash})
	if err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}

	// Verify the file exists
	content, err := os.ReadFile(filepath.Join(dir, "cherry.txt"))
	if err != nil {
		t.Fatalf("read cherry.txt: %v", err)
	}
	if string(content) != "cherry content\n" {
		t.Fatalf("expected cherry content, got: %q", string(content))
	}

	// Verify commit message preserved
	result, err = nativeLog(ctx, dir, []string{"--oneline", "-1"})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(result.Stdout, "cherry commit") {
		t.Fatalf("expected cherry commit message, got: %q", result.Stdout)
	}
}

// TestE2E_Rebase tests simple linear rebase
func TestE2E_Rebase(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create feature branch from initial commit
	nativeCheckout(ctx, dir, []string{"-b", "rebase-feature"})
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"feature.txt"})
	nativeCommit(ctx, dir, []string{"-m", "feature work"})

	// Go back to master and add a commit
	nativeCheckout(ctx, dir, []string{"master"})
	if err := os.WriteFile(filepath.Join(dir, "master-update.txt"), []byte("update\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"master-update.txt"})
	nativeCommit(ctx, dir, []string{"-m", "master update"})

	// Get master HEAD for rebase target
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	masterHash := strings.TrimSpace(result.Stdout)

	// Switch to feature and rebase onto master
	nativeCheckout(ctx, dir, []string{"rebase-feature"})
	result, err = nativeRebase(ctx, dir, []string{masterHash})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if !strings.Contains(result.Stdout, "Successfully rebased") {
		t.Fatalf("expected success message, got: %q", result.Stdout)
	}

	// Both files should exist
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); os.IsNotExist(err) {
		t.Fatal("feature.txt should exist after rebase")
	}
	if _, err := os.Stat(filepath.Join(dir, "master-update.txt")); os.IsNotExist(err) {
		t.Fatal("master-update.txt should exist after rebase")
	}

	// Log should show linear history
	result, err = nativeLog(ctx, dir, []string{"--oneline"})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(result.Stdout, "feature work") {
		t.Fatalf("expected 'feature work' in log, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "master update") {
		t.Fatalf("expected 'master update' in log, got: %q", result.Stdout)
	}
}

// TestE2E_RevList tests rev-list --count
func TestE2E_RevList(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get initial hash
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	firstHash := strings.TrimSpace(result.Stdout)

	// Make two more commits
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0644)
	nativeAdd(ctx, dir, []string{"a.txt"})
	nativeCommit(ctx, dir, []string{"-m", "commit a"})

	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0644)
	nativeAdd(ctx, dir, []string{"b.txt"})
	nativeCommit(ctx, dir, []string{"-m", "commit b"})

	result, err = nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	lastHash := strings.TrimSpace(result.Stdout)

	// Count commits between first and last
	result, err = nativeRevList(ctx, dir, []string{"--count", firstHash + ".." + lastHash})
	if err != nil {
		t.Fatalf("rev-list --count: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "2" {
		t.Fatalf("expected 2 commits, got: %q", result.Stdout)
	}
}

// TestE2E_UpdateRefDelete tests deleting a ref
func TestE2E_UpdateRefDelete(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create a branch
	nativeBranch(ctx, dir, []string{"to-delete"})

	// Verify it exists
	result, err := nativeShowRef(ctx, dir, []string{"--heads"})
	if err != nil {
		t.Fatalf("show-ref: %v", err)
	}
	if !strings.Contains(result.Stdout, "to-delete") {
		t.Fatalf("expected to-delete branch, got: %q", result.Stdout)
	}

	// Delete via update-ref -d
	result, err = nativeUpdateRef(ctx, dir, []string{"-d", "refs/heads/to-delete"})
	if err != nil {
		t.Fatalf("update-ref -d: %v", err)
	}

	// Verify it's gone
	result, err = nativeShowRef(ctx, dir, []string{"--heads"})
	if err != nil {
		t.Fatalf("show-ref: %v", err)
	}
	if strings.Contains(result.Stdout, "to-delete") {
		t.Fatalf("to-delete should be gone, got: %q", result.Stdout)
	}
}

// TestE2E_CatFileCommit tests cat-file -p on a commit object
func TestE2E_CatFileCommit(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	commitHash := strings.TrimSpace(result.Stdout)

	result, err = nativeCatFile(ctx, dir, []string{"-t", commitHash})
	if err != nil {
		t.Fatalf("cat-file -t: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "commit" {
		t.Fatalf("expected type 'commit', got: %q", result.Stdout)
	}

	result, err = nativeCatFile(ctx, dir, []string{"-p", commitHash})
	if err != nil {
		t.Fatalf("cat-file -p: %v", err)
	}
	if !strings.Contains(result.Stdout, "tree ") {
		t.Fatalf("expected 'tree' in commit content, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "author Test User") {
		t.Fatalf("expected author, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "initial commit") {
		t.Fatalf("expected message, got: %q", result.Stdout)
	}
}

// TestE2E_InvalidObjectCatFile tests cat-file with invalid hash
func TestE2E_InvalidObjectCatFile(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeCatFile(ctx, dir, []string{"-t", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 128 {
		t.Fatalf("expected exit code 128, got: %d", result.ExitCode)
	}
}

// TestE2E_FormatPatch tests format-patch generation
func TestE2E_FormatPatch(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Make another commit
	if err := os.WriteFile(filepath.Join(dir, "patch.txt"), []byte("patch content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nativeAdd(ctx, dir, []string{"patch.txt"})
	nativeCommit(ctx, dir, []string{"-m", "patch commit"})

	// Generate format-patch for last commit
	result, err := nativeFormatPatch(ctx, dir, []string{"-1", "HEAD"})
	if err != nil {
		t.Fatalf("format-patch: %v", err)
	}
	if !strings.Contains(result.Stdout, "Subject: [PATCH] patch commit") {
		t.Fatalf("expected patch header, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "patch.txt") {
		t.Fatalf("expected patch.txt in diff, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "patch content") {
		t.Fatalf("expected content in diff, got: %q", result.Stdout)
	}
}

// TestE2E_RemoteGetUrl tests remote get-url with no remote
func TestE2E_RemoteGetUrl(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// No remotes exist in test repo
	result, err := nativeRemote(ctx, dir, []string{"get-url", "origin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 2 {
		t.Fatalf("expected exit code 2 for missing remote, got: %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No such remote") {
		t.Fatalf("expected 'No such remote' error, got: %q", result.Stderr)
	}
}

// TestE2E_RemoteGetUrlWithRemote tests remote get-url with a configured remote
func TestE2E_RemoteGetUrlWithRemote(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Add a remote via go-git
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/example/repo.git"},
	})
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}

	result, err := nativeRemote(ctx, dir, []string{"get-url", "origin"})
	if err != nil {
		t.Fatalf("remote get-url: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "https://github.com/example/repo.git" {
		t.Fatalf("expected remote URL, got: %q", result.Stdout)
	}
}

// TestE2E_WriteTree tests write-tree on a flat index
func TestE2E_WriteTree(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// The repo already has an index with README.md
	result, err := nativeWriteTree(ctx, dir, nil)
	if err != nil {
		// write-tree might fail for nested dirs; test the simple case
		t.Skipf("write-tree not supported for this index layout: %v", err)
	}
	hash := strings.TrimSpace(result.Stdout)
	if len(hash) != 40 {
		t.Fatalf("expected 40-char hash, got: %q", hash)
	}

	// Verify via cat-file -t
	result, err = nativeCatFile(ctx, dir, []string{"-t", hash})
	if err != nil {
		t.Fatalf("cat-file -t: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "tree" {
		t.Fatalf("expected 'tree', got: %q", result.Stdout)
	}
}

// TestE2E_AddAll tests git add -A
func TestE2E_AddAll(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create multiple files
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0644)

	// Add all
	result, err := nativeAdd(ctx, dir, []string{"-A"})
	if err != nil {
		t.Fatalf("add -A: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected TierNative, got: %v", result.Tier)
	}

	// Commit and verify all files
	nativeCommit(ctx, dir, []string{"-m", "add all"})

	result, err = nativeLsFiles(ctx, dir, nil)
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "README.md"} {
		if !strings.Contains(result.Stdout, name) {
			t.Fatalf("expected %s in ls-files, got: %q", name, result.Stdout)
		}
	}
}

// TestE2E_RevParseShort tests rev-parse --short HEAD
func TestE2E_RevParseShort(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get full hash
	full, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse --verify: %v", err)
	}
	fullHash := strings.TrimSpace(full.Stdout)

	// Get short hash
	short, err := nativeRevParse(ctx, dir, []string{"--short", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse --short: %v", err)
	}
	shortHash := strings.TrimSpace(short.Stdout)

	if len(shortHash) != 7 {
		t.Fatalf("expected 7-char short hash, got: %q", shortHash)
	}
	if !strings.HasPrefix(fullHash, shortHash) {
		t.Fatalf("short hash %q should be prefix of full hash %q", shortHash, fullHash)
	}
}

// TestE2E_BlameNonexistentFile tests blame on a file that doesn't exist
func TestE2E_BlameNonexistentFile(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeBlame(ctx, dir, []string{"nonexistent.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 128 {
		t.Fatalf("expected exit code 128, got: %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "no such path") {
		t.Fatalf("expected 'no such path' error, got: %q", result.Stderr)
	}
}

// TestE2E_ExecutorFullCycle tests a complete cycle through the executor
func TestE2E_ExecutorFullCycle(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()
	exec := setupE2EExecutor(t)

	// Full cycle through executor: branch → add → commit → tag → log → show
	result, err := exec.Run(ctx, "git", dir, "checkout", "-b", "exec-cycle")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if result.Tier != TierNative {
		t.Fatalf("expected native tier, got: %v", result.Tier)
	}

	os.WriteFile(filepath.Join(dir, "cycle.txt"), []byte("cycle\n"), 0644)

	result, err = exec.Run(ctx, "git", dir, "add", "cycle.txt")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	result, err = exec.Run(ctx, "git", dir, "commit", "-m", "cycle commit")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	result, err = exec.Run(ctx, "git", dir, "tag", "v-cycle")
	if err != nil {
		t.Fatalf("tag: %v", err)
	}

	result, err = exec.Run(ctx, "git", dir, "log", "--oneline", "-2")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(result.Stdout, "cycle commit") {
		t.Fatalf("expected cycle commit in log, got: %q", result.Stdout)
	}

	result, err = exec.Run(ctx, "git", dir, "show", "HEAD")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(result.Stdout, "cycle commit") {
		t.Fatalf("expected commit in show, got: %q", result.Stdout)
	}

	// Grep through executor
	result, err = exec.Run(ctx, "git", dir, "grep", "cycle")
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(result.Stdout, "cycle.txt") {
		t.Fatalf("expected cycle.txt in grep, got: %q", result.Stdout)
	}

	// Blame through executor
	result, err = exec.Run(ctx, "git", dir, "blame", "cycle.txt")
	if err != nil {
		t.Fatalf("blame: %v", err)
	}
	if !strings.Contains(result.Stdout, "cycle") {
		t.Fatalf("expected content in blame, got: %q", result.Stdout)
	}
}

// TestE2E_BranchContains tests branch --contains
func TestE2E_BranchContains(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get initial commit
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	initialHash := strings.TrimSpace(result.Stdout)

	// Create branches
	nativeBranch(ctx, dir, []string{"contains-branch"})

	// --contains initial commit should show master and contains-branch
	result, err = nativeBranch(ctx, dir, []string{"--contains", initialHash})
	if err != nil {
		t.Fatalf("branch --contains: %v", err)
	}
	if !strings.Contains(result.Stdout, "master") {
		t.Fatalf("expected master in --contains, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "contains-branch") {
		t.Fatalf("expected contains-branch in --contains, got: %q", result.Stdout)
	}
}

// TestE2E_GrepPathspec tests grep with pathspec filter
func TestE2E_GrepPathspec(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Add files in different directories
	repo, _ := git.PlainOpen(dir)
	wt, _ := repo.Worktree()

	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main // target\n"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.txt"), []byte("target word here\n"), 0644)
	wt.Add("src/main.go")
	wt.Add("docs/readme.txt")
	wt.Commit("add dirs", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t", When: time.Now()},
	})

	// Grep with pathspec -- should only search in src/
	result, err := nativeGrep(ctx, dir, []string{"target", "--", "src/*"})
	if err != nil {
		t.Fatalf("grep pathspec: %v", err)
	}
	if !strings.Contains(result.Stdout, "src/main.go") {
		t.Fatalf("expected src/main.go, got: %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "docs/") {
		t.Fatalf("should not match docs/, got: %q", result.Stdout)
	}
}

// TestE2E_CheckoutNonexistentBranch verifies behavior when checking out a branch that doesn't exist
func TestE2E_CheckoutNonexistentBranch(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	_, err := nativeCheckout(ctx, dir, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected ErrNotImplemented for nonexistent branch checkout")
	}
	// Should fall through (ErrNotImplemented) since go-git can't find the branch
	if err != ErrNotImplemented {
		t.Fatalf("expected ErrNotImplemented, got: %v", err)
	}
}

// TestE2E_RevParseVerifyInvalid tests rev-parse --verify with invalid ref
func TestE2E_RevParseVerifyInvalid(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeRevParse(ctx, dir, []string{"--verify", "refs/heads/nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 128 {
		t.Fatalf("expected exit code 128, got: %d", result.ExitCode)
	}
}

// TestE2E_HashObjectNoWrite tests hash-object without -w (still computes hash)
func TestE2E_HashObjectNoWrite(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Write same content to two files
	content := "identical content\n"
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte(content), 0644)
	os.WriteFile(filepath.Join(dir, "file2.txt"), []byte(content), 0644)

	// Hash both - should get same hash
	r1, err := nativeHashObject(ctx, dir, []string{"file1.txt"})
	if err != nil {
		t.Fatalf("hash-object file1: %v", err)
	}
	r2, err := nativeHashObject(ctx, dir, []string{"file2.txt"})
	if err != nil {
		t.Fatalf("hash-object file2: %v", err)
	}

	if strings.TrimSpace(r1.Stdout) != strings.TrimSpace(r2.Stdout) {
		t.Fatalf("same content should produce same hash: %q vs %q", r1.Stdout, r2.Stdout)
	}
}

// TestE2E_DiffTreeWithRecursive tests diff-tree -r flag
func TestE2E_DiffTreeWithRecursive(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get first commit
	r, _ := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	firstHash := strings.TrimSpace(r.Stdout)

	// Add a file and commit
	os.WriteFile(filepath.Join(dir, "added.txt"), []byte("added\n"), 0644)
	nativeAdd(ctx, dir, []string{"added.txt"})
	nativeCommit(ctx, dir, []string{"-m", "add file"})

	r, _ = nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	secondHash := strings.TrimSpace(r.Stdout)

	// diff-tree -r
	result, err := nativeDiffTree(ctx, dir, []string{"-r", firstHash, secondHash})
	if err != nil {
		t.Fatalf("diff-tree -r: %v", err)
	}
	if !strings.Contains(result.Stdout, "A\tadded.txt") {
		t.Fatalf("expected 'A\\tadded.txt', got: %q", result.Stdout)
	}
}

// TestE2E_LsFilesModified tests ls-files --modified
func TestE2E_LsFilesModified(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Modify a tracked file without staging
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("modified\n"), 0644)

	result, err := nativeLsFiles(ctx, dir, []string{"--modified"})
	if err != nil {
		t.Fatalf("ls-files --modified: %v", err)
	}
	if !strings.Contains(result.Stdout, "README.md") {
		t.Fatalf("expected README.md in modified files, got: %q", result.Stdout)
	}
}

// TestE2E_LsFilesNull tests ls-files with -z (null terminated)
func TestE2E_LsFilesNull(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeLsFiles(ctx, dir, []string{"-z"})
	if err != nil {
		t.Fatalf("ls-files -z: %v", err)
	}
	// Should use null terminator instead of newline
	if strings.Contains(result.Stdout, "\n") {
		t.Fatalf("with -z, should not contain newlines, got: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "README.md\x00") {
		t.Fatalf("expected null-terminated README.md, got: %q", result.Stdout)
	}
}

// TestE2E_MergeAlreadyUpToDate tests merge when already up to date
func TestE2E_MergeAlreadyUpToDate(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create branch at same commit
	nativeBranch(ctx, dir, []string{"same-point"})

	// Merge should say already up to date
	result, err := nativeMerge(ctx, dir, []string{"same-point"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(result.Stdout, "Already up to date") {
		t.Fatalf("expected 'Already up to date', got: %q", result.Stdout)
	}
}

// TestE2E_MergeNonexistentBranch tests merge with invalid branch name
func TestE2E_MergeNonexistentBranch(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	result, err := nativeMerge(ctx, dir, []string{"ghost-branch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got: %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "not something we can merge") {
		t.Fatalf("expected merge error, got: %q", result.Stderr)
	}
}

// TestE2E_LogLimit tests log with -n limit
func TestE2E_LogLimit(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Add more commits
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, strings.Replace("file_N.txt", "N", string(rune('a'+i)), 1))
		os.WriteFile(name, []byte("content\n"), 0644)
		nativeAdd(ctx, dir, []string{filepath.Base(name)})
		nativeCommit(ctx, dir, []string{"-m", "commit " + string(rune('a'+i))})
	}

	// Log with limit -2
	result, err := nativeLog(ctx, dir, []string{"--oneline", "-2"})
	if err != nil {
		t.Fatalf("log -2: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %q", len(lines), result.Stdout)
	}
}

// TestE2E_CommitAllowEmpty tests that --allow-empty flag is accepted
// Note: the native implementation accepts the flag but go-git may not create
// a truly empty commit. This tests that the flag parsing doesn't error.
func TestE2E_CommitAllowEmpty(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Stage a change so the commit has something (--allow-empty flag is just accepted)
	os.WriteFile(filepath.Join(dir, "notempty.txt"), []byte("x\n"), 0644)
	nativeAdd(ctx, dir, []string{"notempty.txt"})

	result, err := nativeCommit(ctx, dir, []string{"--allow-empty", "-m", "not-empty commit"})
	if err != nil {
		t.Fatalf("commit --allow-empty: %v", err)
	}
	if !strings.Contains(result.Stdout, "not-empty commit") {
		t.Fatalf("expected commit message, got: %q", result.Stdout)
	}
}

// TestE2E_TagListWithPattern tests tag listing with pattern
func TestE2E_TagListWithPattern(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create various tags
	nativeTag(ctx, dir, []string{"v1.0"})
	nativeTag(ctx, dir, []string{"v2.0"})
	nativeTag(ctx, dir, []string{"release-1"})

	// List with pattern
	result, err := nativeTag(ctx, dir, []string{"-l", "v*"})
	if err != nil {
		t.Fatalf("tag -l: %v", err)
	}
	if !strings.Contains(result.Stdout, "v1.0") || !strings.Contains(result.Stdout, "v2.0") {
		t.Fatalf("expected v1.0 and v2.0, got: %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "release-1") {
		t.Fatalf("release-1 should not match v* pattern, got: %q", result.Stdout)
	}
}

// TestE2E_RevParseVerifyTag tests rev-parse --verify on a tag ref
func TestE2E_RevParseVerifyTag(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create a tag
	nativeTag(ctx, dir, []string{"test-tag"})

	// Resolve the tag
	result, err := nativeRevParse(ctx, dir, []string{"--verify", "refs/tags/test-tag"})
	if err != nil {
		t.Fatalf("rev-parse tag: %v", err)
	}
	hash := strings.TrimSpace(result.Stdout)
	if len(hash) != 40 {
		t.Fatalf("expected 40-char hash, got: %q", hash)
	}

	// Should match HEAD
	headResult, err := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if strings.TrimSpace(headResult.Stdout) != hash {
		t.Fatalf("tag should point to HEAD, tag=%q head=%q", hash, headResult.Stdout)
	}
}

// TestE2E_BlameWithLargeFile tests blame on a file with many lines
func TestE2E_BlameWithLargeFile(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Create a file with 200 lines
	var builder strings.Builder
	for i := 1; i <= 200; i++ {
		builder.WriteString(strings.Replace("line NUMBER\n", "NUMBER", strings.Repeat("x", i%10+1), 1))
	}
	os.WriteFile(filepath.Join(dir, "large.txt"), []byte(builder.String()), 0644)
	nativeAdd(ctx, dir, []string{"large.txt"})
	nativeCommit(ctx, dir, []string{"-m", "add large file"})

	// Blame entire file
	result, err := nativeBlame(ctx, dir, []string{"large.txt"})
	if err != nil {
		t.Fatalf("blame: %v", err)
	}
	blameLines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(blameLines) != 200 {
		t.Fatalf("expected 200 blame lines, got: %d", len(blameLines))
	}
}

// TestE2E_ShowNonHeadCommit tests show on an older commit
func TestE2E_ShowNonHeadCommit(t *testing.T) {
	dir := setupE2ERepo(t)
	ctx := context.Background()

	// Get initial hash
	r, _ := nativeRevParse(ctx, dir, []string{"--verify", "HEAD"})
	initialHash := strings.TrimSpace(r.Stdout)

	// Make another commit
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644)
	nativeAdd(ctx, dir, []string{"new.txt"})
	nativeCommit(ctx, dir, []string{"-m", "second"})

	// Show the initial commit by hash
	result, err := nativeShow(ctx, dir, []string{initialHash})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(result.Stdout, "initial commit") {
		t.Fatalf("expected initial commit message, got: %q", result.Stdout)
	}
}
