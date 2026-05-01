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

// setupTestRepo creates a temp directory with an initialized git repo and one commit.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Create a file and commit it
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	if _, err := wt.Add("hello.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}

	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	return dir
}

func TestNativeRevParse_IsInsideWorkTree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeRevParse(context.Background(), dir, []string{"--is-inside-work-tree"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "true\n" {
		t.Errorf("got %q, want %q", result.Stdout, "true\n")
	}
}

func TestNativeRevParse_ShowToplevel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeRevParse(context.Background(), dir, []string{"--show-toplevel"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(result.Stdout)
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestNativeRevParse_AbbrevRefHEAD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeRevParse(context.Background(), dir, []string{"--abbrev-ref", "HEAD"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(result.Stdout)
	if got != "master" {
		t.Errorf("got %q, want %q", got, "master")
	}
}

func TestNativeRevParse_ShortHEAD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeRevParse(context.Background(), dir, []string{"--short", "HEAD"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(result.Stdout)
	if len(got) != 7 {
		t.Errorf("expected 7-char short hash, got %q (len=%d)", got, len(got))
	}
}

func TestNativeStatus_Clean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeStatus(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "" {
		t.Errorf("expected empty status for clean repo, got %q", result.Stdout)
	}
}

func TestNativeStatus_Modified(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	// Modify the file
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("modified\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := nativeStatus(context.Background(), dir, []string{"--short"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello.txt") {
		t.Errorf("expected hello.txt in status output, got %q", result.Stdout)
	}
}

func TestNativeLog_Oneline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeLog(context.Background(), dir, []string{"--oneline", "-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "initial commit") {
		t.Errorf("expected 'initial commit' in output, got %q", result.Stdout)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
}

func TestNativeLog_Default(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeLog(context.Background(), dir, []string{"-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "commit ") {
		t.Errorf("expected 'commit ' in output, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Author: Test User") {
		t.Errorf("expected author in output, got %q", result.Stdout)
	}
}

func TestNativeAdd_And_Commit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	// Create a new file
	newFile := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(newFile, []byte("new content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Add it
	result, err := nativeAdd(context.Background(), dir, []string{"new.txt"})
	if err != nil {
		t.Fatalf("add error: %v", err)
	}
	if result.Tier != TierNative {
		t.Errorf("expected TierNative, got %v", result.Tier)
	}

	// Commit it
	result, err = nativeCommit(context.Background(), dir, []string{"-m", "add new file"})
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if !strings.Contains(result.Stdout, "add new file") {
		t.Errorf("expected commit message in output, got %q", result.Stdout)
	}
}

func TestNativeBranch_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeBranch(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("branch error: %v", err)
	}
	if !strings.Contains(result.Stdout, "master") {
		t.Errorf("expected 'master' in branch list, got %q", result.Stdout)
	}
}

func TestNativeBranch_CreateAndCheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	// Create branch
	result, err := nativeBranch(context.Background(), dir, []string{"feature"})
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if result.Tier != TierNative {
		t.Errorf("expected TierNative")
	}

	// Checkout
	result, err = nativeCheckout(context.Background(), dir, []string{"feature"})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if !strings.Contains(result.Stdout, "feature") {
		t.Errorf("expected 'feature' in output, got %q", result.Stdout)
	}

	// Verify HEAD
	result, err = nativeRevParse(context.Background(), dir, []string{"--abbrev-ref", "HEAD"})
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "feature" {
		t.Errorf("expected HEAD on 'feature', got %q", result.Stdout)
	}
}

func TestNativeCheckout_CreateBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	result, err := nativeCheckout(context.Background(), dir, []string{"-b", "new-branch"})
	if err != nil {
		t.Fatalf("checkout -b: %v", err)
	}
	if !strings.Contains(result.Stdout, "new-branch") {
		t.Errorf("expected 'new-branch' in output, got %q", result.Stdout)
	}
}

func TestNativeRemote_GetURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	// Add a remote
	repo, _ := git.PlainOpen(dir)
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/test/repo.git"},
	})
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}

	result, err := nativeRemote(context.Background(), dir, []string{"get-url", "origin"})
	if err != nil {
		t.Fatalf("remote get-url: %v", err)
	}
	expected := "https://github.com/test/repo.git\n"
	if result.Stdout != expected {
		t.Errorf("got %q, want %q", result.Stdout, expected)
	}
}

func TestNativeStash_ReturnsNotImplemented(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	_, err := nativeStash(context.Background(), "", nil)
	if err != ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

func TestNativeMerge_ReturnsNotImplemented(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	_, err := nativeMerge(context.Background(), "", nil)
	if err != ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

func TestNativeMergeBase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := setupTestRepo(t)

	// Create a second commit on a branch
	repo, _ := git.PlainOpen(dir)
	wt, _ := repo.Worktree()

	// Get current HEAD hash (will be the merge base)
	head, _ := repo.Head()
	baseHash := head.Hash().String()

	// Create branch and add a commit
	err := wt.Checkout(&git.CheckoutOptions{
		Branch: "refs/heads/feature",
		Create: true,
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	wt.Add("feature.txt")
	wt.Commit("feature commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@t.com", When: time.Now()},
	})

	// merge-base between master and feature should be the initial commit
	result, err := nativeMergeBase(context.Background(), dir, []string{"master", "feature"})
	if err != nil {
		t.Fatalf("merge-base: %v", err)
	}
	got := strings.TrimSpace(result.Stdout)
	if got != baseHash {
		t.Errorf("got %q, want %q", got, baseHash)
	}
}
