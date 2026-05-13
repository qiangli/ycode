package selfinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepo creates a temp directory with `git init` run inside.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

var testCaps = []CapabilitySpec{
	{Name: "ycode-stdio", Transport: "stdio", Command: "ycode", Args: []string{"mcp", "serve"}, Family: "stdio"},
	{Name: "ycode-loom", Transport: "http", URL: "http://127.0.0.1:58080/loom-mcp/", Family: "loom"},
}

// WriteProjectFiles is intentionally minimal: writes ONLY the
// foreign-agent breadcrumb at <repo>/.agents/ycode/AGENTS.md. Root
// AGENTS.md / CLAUDE.md and docs/backlog.md are never touched.
func TestWriteProjectFiles_BreadcrumbOnly(t *testing.T) {
	repo := makeRepo(t)

	written, warnings, err := WriteProjectFiles(repo, testCaps)
	if err != nil {
		t.Fatalf("WriteProjectFiles: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	breadcrumb := filepath.Join(repo, ".agents", "ycode", "AGENTS.md")
	if len(written) != 1 || written[0] != breadcrumb {
		t.Fatalf("expected single write of %s, got %v", breadcrumb, written)
	}

	// Confirm content was actually written and references ycode capabilities.
	body, err := os.ReadFile(breadcrumb)
	if err != nil {
		t.Fatalf("read breadcrumb: %v", err)
	}
	if !strings.Contains(string(body), "ycode-loom") {
		t.Errorf("breadcrumb does not list capabilities:\n%s", body)
	}

	// Critically: must NOT have touched these.
	for _, p := range []string{
		filepath.Join(repo, "AGENTS.md"),
		filepath.Join(repo, "CLAUDE.md"),
		filepath.Join(repo, "docs", "backlog.md"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("ycode should not have created %s (stat err=%v)", p, err)
		}
	}
}

func TestWriteProjectFiles_DoesNotPatchExistingRoot(t *testing.T) {
	repo := makeRepo(t)

	// User authored both root files; ycode must leave them alone.
	agents := filepath.Join(repo, "AGENTS.md")
	claude := filepath.Join(repo, "CLAUDE.md")
	agentsContent := "# AGENTS.md\n\nUser content.\n"
	claudeContent := "# CLAUDE.md\n\nClaude note.\n"
	if err := os.WriteFile(agents, []byte(agentsContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte(claudeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatalf("WriteProjectFiles: %v", err)
	}

	gotAgents, _ := os.ReadFile(agents)
	if string(gotAgents) != agentsContent {
		t.Errorf("AGENTS.md was modified:\nbefore=%q\nafter=%q", agentsContent, gotAgents)
	}
	gotClaude, _ := os.ReadFile(claude)
	if string(gotClaude) != claudeContent {
		t.Errorf("CLAUDE.md was modified:\nbefore=%q\nafter=%q", claudeContent, gotClaude)
	}
}

func TestWriteProjectFiles_Idempotent(t *testing.T) {
	repo := makeRepo(t)
	breadcrumb := filepath.Join(repo, ".agents", "ycode", "AGENTS.md")

	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatalf("first run: %v", err)
	}
	body1, _ := os.ReadFile(breadcrumb)

	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatalf("second run: %v", err)
	}
	body2, _ := os.ReadFile(breadcrumb)

	if string(body1) != string(body2) {
		t.Errorf("not idempotent\nfirst:\n%s\nsecond:\n%s", body1, body2)
	}
}

func TestRootPointerSnippet(t *testing.T) {
	s := RootPointerSnippet()
	if !strings.Contains(s, ".agents/ycode/AGENTS.md") {
		t.Errorf("snippet should reference the breadcrumb: %q", s)
	}
	if !strings.Contains(s, "ycode init --refresh") {
		t.Errorf("snippet should mention the refresh command: %q", s)
	}
}

func TestFindGitRoot(t *testing.T) {
	repo := makeRepo(t)
	nested := filepath.Join(repo, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindGitRoot(nested); got != repo {
		t.Errorf("FindGitRoot=%q want %q", got, repo)
	}
	if got := FindGitRoot(t.TempDir()); got != "" {
		t.Errorf("FindGitRoot in non-repo: %q", got)
	}
}
