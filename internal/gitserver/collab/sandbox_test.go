package collab

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/gitserver/agents"
)

// TestPrepareSandbox_LocalRepo exercises sandbox prep against a
// purely-local bare git repo (no Gitea, no HTTP). Verifies that:
//   - the sandbox dir is created
//   - the agent branch is checked out
//   - user.name/user.email are configured to the agent's identity
func TestPrepareSandbox_LocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not in PATH")
	}

	// Build a tiny bare repo with one commit on main, served at file:// URL.
	bare := setupBareRepoWithMain(t)

	a := &agents.Agent{ID: "agent-test", Name: "test"}
	root := t.TempDir()

	dir, err := PrepareSandbox(context.Background(), root, "file://"+bare, "ignored-token", a, 42, "agent/agent-test/issue-42")
	if err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}

	want := filepath.Join(root, "agent-test", "issue-42")
	if dir != want {
		t.Errorf("sandbox dir: got %q want %q", dir, want)
	}

	// Verify the branch.
	out := mustGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if strings.TrimSpace(out) != "agent/agent-test/issue-42" {
		t.Errorf("HEAD branch: got %q want agent/agent-test/issue-42", out)
	}

	// Verify author identity.
	if got := strings.TrimSpace(mustGit(t, dir, "config", "user.name")); got != "agent-test" {
		t.Errorf("user.name: got %q want agent-test", got)
	}
	if got := strings.TrimSpace(mustGit(t, dir, "config", "user.email")); got != "agent-test@ycode.local" {
		t.Errorf("user.email: got %q want agent-test@ycode.local", got)
	}

	// Verify the original commit is reachable (we cloned main).
	if out := mustGit(t, dir, "log", "--oneline", "main"); !strings.Contains(out, "initial") {
		t.Errorf("expected initial commit, got: %s", out)
	}
}

func TestPrepareSandbox_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not in PATH")
	}
	bare := setupBareRepoWithMain(t)
	a := &agents.Agent{ID: "agent-x"}
	root := t.TempDir()

	if _, err := PrepareSandbox(context.Background(), root, "file://"+bare, "ignored", a, 1, "agent/agent-x/issue-1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Second call must wipe + recreate without error.
	if _, err := PrepareSandbox(context.Background(), root, "file://"+bare, "ignored", a, 1, "agent/agent-x/issue-1"); err != nil {
		t.Fatalf("second: %v", err)
	}
}

func TestInjectToken(t *testing.T) {
	cases := []struct {
		in    string
		token string
		want  string
	}{
		{"http://localhost:3000/admin/x.git", "tok", "http://token:tok@localhost:3000/admin/x.git"},
		{"https://gitea.local/admin/x.git", "tok", "https://token:tok@gitea.local/admin/x.git"},
		{"file:///tmp/repo.git", "tok", "file:///tmp/repo.git"},
		{"git@host:foo/bar.git", "tok", "git@host:foo/bar.git"},
	}
	for _, c := range cases {
		if got := injectToken(c.in, c.token); got != c.want {
			t.Errorf("injectToken(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

// setupBareRepoWithMain builds a working bare repo containing one
// initial commit on main. Returns the bare repo's path (suitable
// for use as a file:// URL).
func setupBareRepoWithMain(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	mustExec(t, src, "git", "init", "-b", "main")
	mustExec(t, src, "git", "config", "user.name", "tester")
	mustExec(t, src, "git", "config", "user.email", "tester@example.com")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	mustExec(t, src, "git", "add", "README.md")
	mustExec(t, src, "git", "commit", "-m", "initial")

	bare := t.TempDir()
	mustExec(t, "", "git", "clone", "--bare", src, bare)
	return bare
}

func mustExec(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
