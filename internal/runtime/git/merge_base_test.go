package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
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

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	// Create feature branch with a new commit.
	run("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature commit")

	return dir
}

func TestMergeBase(t *testing.T) {
	dir := setupTestRepo(t)

	base, err := MergeBase(dir, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("expected non-empty merge base")
	}
	if len(base) < 7 {
		t.Errorf("expected full hash, got %q", base)
	}
}

func TestDiffStat(t *testing.T) {
	dir := setupTestRepo(t)

	// Switch to feature branch for HEAD to work.
	cmd := exec.Command("git", "checkout", "feature")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	stat, err := DiffStat(dir, "", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if stat == "" {
		t.Error("expected non-empty diff stat")
	}
	// Should mention feature.txt.
	if !contains(stat, "feature.txt") {
		t.Errorf("expected diff stat to mention feature.txt, got %q", stat)
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
