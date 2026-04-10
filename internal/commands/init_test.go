package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeRepoCreatesExpectedFiles(t *testing.T) {
	root := t.TempDir()

	// Simulate a Go project.
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "cmd"), 0o755)
	os.MkdirAll(filepath.Join(root, "internal"), 0o755)

	report, err := InitializeRepo(root)
	if err != nil {
		t.Fatalf("InitializeRepo failed: %v", err)
	}

	rendered := report.Render()
	if !strings.Contains(rendered, ".ycode/") {
		t.Error("report should mention .ycode/")
	}
	if !strings.Contains(rendered, ".ycode.json") {
		t.Error("report should mention .ycode.json")
	}
	if !strings.Contains(rendered, "created") {
		t.Error("report should show created status")
	}
	if !strings.Contains(rendered, "CLAUDE.md") {
		t.Error("report should mention CLAUDE.md")
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(root, ".ycode")); err != nil {
		t.Error(".ycode/ directory was not created")
	}
	if _, err := os.Stat(filepath.Join(root, ".ycode.json")); err != nil {
		t.Error(".ycode.json was not created")
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Error("CLAUDE.md was not created")
	}

	// Verify .ycode.json content.
	data, _ := os.ReadFile(filepath.Join(root, ".ycode.json"))
	if !strings.Contains(string(data), "defaultMode") {
		t.Error(".ycode.json should contain defaultMode")
	}

	// Verify .gitignore entries.
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	giStr := string(gi)
	if !strings.Contains(giStr, ".ycode/settings.local.json") {
		t.Error(".gitignore should contain .ycode/settings.local.json")
	}
	if !strings.Contains(giStr, ".ycode/sessions/") {
		t.Error(".gitignore should contain .ycode/sessions/")
	}

	// Verify CLAUDE.md detects Go.
	md, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	mdStr := string(md)
	if !strings.Contains(mdStr, "Go") {
		t.Error("CLAUDE.md should detect Go language")
	}
	if !strings.Contains(mdStr, "go test") {
		t.Error("CLAUDE.md should include Go verification commands")
	}
	if !strings.Contains(mdStr, "`cmd/`") {
		t.Error("CLAUDE.md should mention cmd/ directory")
	}
	if !strings.Contains(mdStr, "`internal/`") {
		t.Error("CLAUDE.md should mention internal/ directory")
	}
}

func TestInitializeRepoIsIdempotent(t *testing.T) {
	root := t.TempDir()

	// Pre-create CLAUDE.md with custom content.
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("custom guidance\n"), 0o644)
	// Pre-create .gitignore with one entry already present.
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".ycode/settings.local.json\n"), 0o644)

	first, err := InitializeRepo(root)
	if err != nil {
		t.Fatalf("first init failed: %v", err)
	}
	firstRendered := first.Render()
	if !strings.Contains(firstRendered, "CLAUDE.md") || !strings.Contains(firstRendered, "skipped (already exists)") {
		t.Error("first init should skip existing CLAUDE.md")
	}

	second, err := InitializeRepo(root)
	if err != nil {
		t.Fatalf("second init failed: %v", err)
	}
	secondRendered := second.Render()
	if !strings.Contains(secondRendered, "skipped (already exists)") {
		t.Error("second init should skip everything")
	}

	// Verify CLAUDE.md was NOT overwritten.
	md, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(md) != "custom guidance\n" {
		t.Error("CLAUDE.md should not be overwritten")
	}

	// Verify .gitignore doesn't duplicate entries.
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	giStr := string(gi)
	if strings.Count(giStr, ".ycode/settings.local.json") != 1 {
		t.Error(".gitignore should not have duplicate entries")
	}
}

func TestRenderInitClaudeMDDetectsGoProject(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644)

	md := RenderInitClaudeMD(root)
	if !strings.Contains(md, "Languages: Go.") {
		t.Errorf("should detect Go, got:\n%s", md)
	}
	if !strings.Contains(md, "go test") {
		t.Error("should include Go verification commands")
	}
}

func TestRenderInitClaudeMDDetectsRustProject(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "rust"), 0o755)
	os.WriteFile(filepath.Join(root, "rust", "Cargo.toml"), []byte("[workspace]\n"), 0o644)

	md := RenderInitClaudeMD(root)
	if !strings.Contains(md, "Languages: Rust.") {
		t.Errorf("should detect Rust, got:\n%s", md)
	}
	if !strings.Contains(md, "cargo clippy") {
		t.Error("should include Rust verification commands")
	}
}

func TestRenderInitClaudeMDDetectsPythonAndNextJS(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"demo\"\n"), 0o644)
	os.WriteFile(filepath.Join(root, "package.json"),
		[]byte(`{"dependencies":{"next":"14.0.0","react":"18.0.0"},"devDependencies":{"typescript":"5.0.0"}}`),
		0o644)

	md := RenderInitClaudeMD(root)
	if !strings.Contains(md, "Python") {
		t.Error("should detect Python")
	}
	if !strings.Contains(md, "TypeScript") {
		t.Error("should detect TypeScript")
	}
	if !strings.Contains(md, "Next.js") {
		t.Error("should detect Next.js")
	}
	if !strings.Contains(md, "React") {
		t.Error("should detect React")
	}
	if !strings.Contains(md, "pyproject.toml") {
		t.Error("should mention pyproject.toml")
	}
}

func TestRenderInitClaudeMDNoLanguagesDetected(t *testing.T) {
	root := t.TempDir()

	md := RenderInitClaudeMD(root)
	if !strings.Contains(md, "No specific language markers") {
		t.Error("should report no languages detected")
	}
	if !strings.Contains(md, "Frameworks: none") {
		t.Error("should report no frameworks detected")
	}
}

func TestEnsureGitignoreCreatesNewFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")

	status, err := ensureGitignoreEntries(path)
	if err != nil {
		t.Fatalf("ensureGitignoreEntries failed: %v", err)
	}
	if status != InitCreated {
		t.Error("should return Created for new file")
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, gitignoreComment) {
		t.Error("should contain comment header")
	}
	for _, entry := range gitignoreEntries {
		if !strings.Contains(content, entry) {
			t.Errorf("should contain entry %q", entry)
		}
	}
}

func TestEnsureGitignoreUpdatesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	os.WriteFile(path, []byte("*.log\n"), 0o644)

	status, err := ensureGitignoreEntries(path)
	if err != nil {
		t.Fatalf("ensureGitignoreEntries failed: %v", err)
	}
	if status != InitUpdated {
		t.Error("should return Updated when adding entries")
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "*.log") {
		t.Error("should preserve existing entries")
	}
	if !strings.Contains(content, gitignoreComment) {
		t.Error("should add comment header")
	}
}

func TestEnsureGitignoreSkipsWhenComplete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	lines := append([]string{gitignoreComment}, gitignoreEntries...)
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	status, err := ensureGitignoreEntries(path)
	if err != nil {
		t.Fatalf("ensureGitignoreEntries failed: %v", err)
	}
	if status != InitSkipped {
		t.Error("should return Skipped when all entries present")
	}
}
