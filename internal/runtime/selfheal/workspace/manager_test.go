package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareSourceRepo creates a one-commit local "upstream" git repo
// that EnsureClone can clone from, side-stepping network access in
// the test sandbox.
func initBareSourceRepo(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "upstream-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, src, "git", "init", "-q", "-b", "main")
	mustRun(t, src, "git", "-c", "user.email=t@t", "-c", "user.name=t", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(src, "README"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, src, "git", "add", "README")
	mustRun(t, src, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init")
	return src
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestManager_EnsureClone_FreshThenFetch(t *testing.T) {
	src := initBareSourceRepo(t)
	base := t.TempDir()
	layout := PathsFor(base, "sig123")
	m := New()

	if err := m.EnsureClone(context.Background(), layout, src); err != nil {
		t.Fatalf("first EnsureClone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.Clone, ".git")); err != nil {
		t.Fatalf("clone missing .git: %v", err)
	}

	// Second call: existing clone path → fetch. Should be no-error,
	// no re-clone.
	stat, _ := os.Stat(layout.Clone)
	if err := m.EnsureClone(context.Background(), layout, src); err != nil {
		t.Fatalf("second EnsureClone (idempotent): %v", err)
	}
	stat2, _ := os.Stat(layout.Clone)
	if stat.ModTime().After(stat2.ModTime()) {
		t.Fatalf("clone dir mtime moved backward; suggests re-clone happened")
	}
}

func TestManager_EnsureClone_RefusesURLMismatch(t *testing.T) {
	src1 := initBareSourceRepo(t)
	src2 := initBareSourceRepo(t)
	base := t.TempDir()
	layout := PathsFor(base, "sigABC")
	m := New()
	if err := m.EnsureClone(context.Background(), layout, src1); err != nil {
		t.Fatalf("EnsureClone src1: %v", err)
	}
	if err := m.EnsureClone(context.Background(), layout, src2); err == nil {
		t.Fatalf("expected error when re-using clone dir with a different URL; got nil")
	}
}

func TestManager_CreateWorktree_Idempotent(t *testing.T) {
	src := initBareSourceRepo(t)
	base := t.TempDir()
	layout := PathsFor(base, "sig-wt")
	m := New()
	if err := m.EnsureClone(context.Background(), layout, src); err != nil {
		t.Fatal(err)
	}
	// Clone was --no-checkout; need at least one fetched ref. Already
	// have origin/main from the upstream.
	wt, err := m.CreateWorktree(context.Background(), layout, "selfheal/test-branch")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README")); err != nil {
		t.Fatalf("worktree missing README: %v", err)
	}

	wt2, err := m.CreateWorktree(context.Background(), layout, "selfheal/test-branch")
	if err != nil {
		t.Fatalf("second CreateWorktree: %v", err)
	}
	if wt != wt2 {
		t.Fatalf("worktree path changed across re-attempts: %s vs %s", wt, wt2)
	}
}

func TestDiscoverFork_Precedence(t *testing.T) {
	m := New()
	// Env var wins.
	t.Setenv("SELFHEAL_REPO", "https://github.com/ME/myfork.git")
	got, err := m.DiscoverFork(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/ME/myfork.git" {
		t.Fatalf("env-var precedence broken: %s", got)
	}

	// Without env, falls back to upstream.
	t.Setenv("SELFHEAL_REPO", "")
	// Use a tempdir as "fallbackDir" that's NOT a git repo so the
	// `git config` lookup misses and the upstream fallback fires.
	got, err = m.DiscoverFork(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/qiangli/ycode.git" {
		t.Fatalf("upstream fallback broken: %s", got)
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":     "https://github.com/owner/repo.git",
		"ssh://git@github.com/owner/repo":   "https://github.com/owner/repo",
		"https://github.com/owner/repo.git": "https://github.com/owner/repo.git",
	}
	for in, want := range cases {
		if got := normalizeRepoURL(in); got != want {
			t.Errorf("normalizeRepoURL(%q) = %q; want %q", in, got, want)
		}
	}
}
