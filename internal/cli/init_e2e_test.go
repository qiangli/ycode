//go:build e2e

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E tests for `ycode init`. Each test runs the real binary in a fresh
// $TMPDIR-backed git repo with HOME isolated to t.TempDir() so the
// user-global ~/.config/ycode writes don't leak between tests or touch
// the developer's actual machine.
//
// Run: make test-tui-e2e   (or)   go test -tags e2e -count=1 ./internal/cli/...

// initRepo creates a fresh git repo in tmp, returns its abs path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-q"},
		{"touch", "seed"},
		{"git", "add", "seed"},
		{"git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "seed"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return dir
}

// runYcode runs `bin/ycode <args...>` with HOME isolated; returns
// stdout+stderr combined.
func runYcode(t *testing.T, repoDir, home string, args ...string) (string, error) {
	t.Helper()
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	binAbs, err := filepath.Abs(e2eBinaryPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cmd := exec.Command(binAbs, args...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TERM=dumb",
		"YCODE_NO_SERVER=1",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestE2E_Init_FreshRepo_WritesAllFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	home := t.TempDir()

	out, err := runYcode(t, repo, home, "init")
	if err != nil {
		t.Fatalf("ycode init: %v\n%s", err, out)
	}

	// Project-scope files.
	for _, p := range []string{
		"AGENTS.md",
		".agents/ycode/AGENTS.md",
		".agents/ycode/.init-done",
		"docs/backlog.md",
		"docs/backlog/README.md",
	} {
		if _, err := os.Stat(filepath.Join(repo, p)); err != nil {
			t.Errorf("expected %s after init: %v\noutput:\n%s", p, err, out)
		}
	}
	// User-global Foreman skill.
	skill := filepath.Join(home, ".config", "ycode", "skills", "ycode-foreman", "skill.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("expected user-global skill at %s: %v", skill, err)
	}
}

func TestE2E_Init_Idempotent_MarkerSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	home := t.TempDir()

	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	out, err := runYcode(t, repo, home, "init")
	if err != nil {
		t.Fatalf("second init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "marker matches") {
		t.Errorf("second init should skip on marker; got:\n%s", out)
	}
}

func TestE2E_Init_Refresh_NoContentDrift_NoMtimeChange(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	home := t.TempDir()

	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	files := []string{
		filepath.Join(repo, "AGENTS.md"),
		filepath.Join(repo, "docs/backlog.md"),
		filepath.Join(home, ".config/ycode/skills/ycode-foreman/skill.md"),
	}
	before := make(map[string]int64)
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		before[f] = st.ModTime().UnixNano()
	}

	// Wait long enough that any rewrite would shift mtime past sub-second resolution.
	time.Sleep(2 * time.Second)
	if _, err := runYcode(t, repo, home, "init", "--refresh"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat after refresh %s: %v", f, err)
		}
		if st.ModTime().UnixNano() != before[f] {
			t.Errorf("mtime drifted for %s — content-stable refresh should be a no-op", f)
		}
	}
}

func TestE2E_Init_SelfHealsDeletedUserSkill(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	home := t.TempDir()
	skill := filepath.Join(home, ".config/ycode/skills/ycode-foreman/skill.md")

	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := os.Remove(skill); err != nil {
		t.Fatalf("remove skill: %v", err)
	}

	// In a different fresh repo for the same HOME, init must restore the
	// user-global skill — even though the previous repo's marker may match.
	repo2 := initRepo(t)
	if _, err := runYcode(t, repo2, home, "init"); err != nil {
		t.Fatalf("init in second repo: %v", err)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("user-global skill not restored: %v", err)
	}
}

func TestE2E_Init_PreservesUserEditedReadme(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	home := t.TempDir()
	readme := filepath.Join(repo, "docs/backlog/README.md")

	if _, err := runYcode(t, repo, home, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	custom := []byte("# my custom backlog notes\n")
	if err := os.WriteFile(readme, custom, 0o644); err != nil {
		t.Fatalf("write custom readme: %v", err)
	}
	if _, err := runYcode(t, repo, home, "init", "--refresh"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	if string(got) != string(custom) {
		t.Errorf("README.md was overwritten; want %q got %q", custom, got)
	}
}
