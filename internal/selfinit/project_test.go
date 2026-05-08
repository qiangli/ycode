package selfinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepo creates a temp directory with `git init` run inside. Used
// to exercise FindGitRoot and the project-scope writers without real
// repositories.
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

func TestWriteProjectFiles_Greenfield(t *testing.T) {
	repo := makeRepo(t)

	written, _, err := WriteProjectFiles(repo, testCaps)
	if err != nil {
		t.Fatalf("WriteProjectFiles: %v", err)
	}
	// Greenfield should write .ycode/AGENTS.md AND a fresh AGENTS.md
	// (owned, no delimiters).
	wantWritten := map[string]bool{
		filepath.Join(repo, ".ycode", "AGENTS.md"): true,
		filepath.Join(repo, "AGENTS.md"):           true,
	}
	for _, w := range written {
		delete(wantWritten, w)
	}
	if len(wantWritten) > 0 {
		t.Errorf("expected greenfield to write both files, missing: %v", wantWritten)
	}

	body, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if !IsOwnedFile(string(body)) {
		t.Errorf("greenfield AGENTS.md should carry OwnedMarker, got:\n%s", body)
	}
	if HasBlock(string(body)) {
		t.Errorf("greenfield AGENTS.md should not have BEGIN/END markers, got:\n%s", body)
	}
	if !strings.Contains(string(body), "ycode-loom") {
		t.Errorf("expected long-form content with capability list, got:\n%s", body)
	}
}

func TestWriteProjectFiles_BrownfieldExistingAgents(t *testing.T) {
	repo := makeRepo(t)
	existing := "# AGENTS.md\n\nUser-curated content goes here.\n"
	agentsPath := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatalf("WriteProjectFiles: %v", err)
	}

	body, _ := os.ReadFile(agentsPath)
	bodyStr := string(body)
	if !strings.HasPrefix(bodyStr, "# AGENTS.md") {
		t.Errorf("user content lost: %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "User-curated content goes here.") {
		t.Errorf("user content missing: %q", bodyStr)
	}
	if !HasBlock(bodyStr) {
		t.Errorf("BEGIN/END block missing: %q", bodyStr)
	}
	if IsOwnedFile(bodyStr) {
		t.Errorf("brownfield should not carry OwnedMarker")
	}
}

func TestWriteProjectFiles_BrownfieldClaudeMd(t *testing.T) {
	repo := makeRepo(t)
	existing := "# CLAUDE.md\n\nClaude-specific note.\n"
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	written, _, err := WriteProjectFiles(repo, testCaps)
	if err != nil {
		t.Fatalf("WriteProjectFiles: %v", err)
	}
	// Should patch CLAUDE.md, NOT create AGENTS.md.
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md should not be created when CLAUDE.md exists")
	}
	wantPatched := false
	for _, w := range written {
		if filepath.Base(w) == "CLAUDE.md" {
			wantPatched = true
		}
	}
	if !wantPatched {
		t.Errorf("CLAUDE.md not patched: %v", written)
	}
	body, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if !HasBlock(string(body)) || !strings.Contains(string(body), "# CLAUDE.md") {
		t.Errorf("CLAUDE.md unexpected:\n%s", body)
	}
}

func TestWriteProjectFiles_Idempotent(t *testing.T) {
	repo := makeRepo(t)

	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatalf("first run: %v", err)
	}
	body1, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))

	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatalf("second run: %v", err)
	}
	body2, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))

	if string(body1) != string(body2) {
		t.Errorf("not idempotent\nfirst:\n%s\nsecond:\n%s", body1, body2)
	}
}

func TestWriteProjectFiles_MarkerRemoval_DegradesToBrownfield(t *testing.T) {
	repo := makeRepo(t)

	// First run: greenfield, file is owned.
	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(repo, "AGENTS.md")
	body, _ := os.ReadFile(agentsPath)
	if !IsOwnedFile(string(body)) {
		t.Fatal("expected owned after greenfield")
	}

	// User reclaims the file by removing the OwnedMarker line.
	stripped := strings.Replace(string(body), OwnedMarker+"\n", "", 1)
	stripped = strings.TrimLeft(stripped, "\n")
	if err := os.WriteFile(agentsPath, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second run: file is now brownfield; we should splice rather
	// than re-take ownership.
	if _, _, err := WriteProjectFiles(repo, testCaps); err != nil {
		t.Fatal(err)
	}
	body2, _ := os.ReadFile(agentsPath)
	if IsOwnedFile(string(body2)) {
		t.Errorf("ycode reclaimed user-owned file: %s", body2)
	}
	if !HasBlock(string(body2)) {
		t.Errorf("expected brownfield delimited block, got:\n%s", body2)
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
