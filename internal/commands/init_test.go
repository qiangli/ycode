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
	if !strings.Contains(rendered, ".agents/ycode/") {
		t.Error("report should mention .agents/ycode/")
	}
	if !strings.Contains(rendered, ".agents/ycode.json") {
		t.Error("report should mention .agents/ycode.json")
	}
	if !strings.Contains(rendered, "created") {
		t.Error("report should show created status")
	}
	if !strings.Contains(rendered, "CLAUDE.md") {
		t.Error("report should mention CLAUDE.md")
	}
	if !strings.Contains(rendered, "AGENTS.md") {
		t.Error("report should mention AGENTS.md")
	}
	if !strings.Contains(rendered, "Git") {
		t.Error("report should mention Git status")
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(root, ".agents", "ycode")); err != nil {
		t.Error(".agents/ycode/ directory was not created")
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "ycode.json")); err != nil {
		t.Error(".agents/ycode.json was not created")
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Error("CLAUDE.md was not created")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Error("AGENTS.md was not created")
	}

	// Verify .agents/ycode.json content.
	data, _ := os.ReadFile(filepath.Join(root, ".agents", "ycode.json"))
	if !strings.Contains(string(data), "defaultMode") {
		t.Error(".agents/ycode.json should contain defaultMode")
	}
	if !strings.Contains(string(data), "languages") {
		t.Error(".agents/ycode.json should contain languages")
	}

	// Verify .gitignore entries.
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	giStr := string(gi)
	if !strings.Contains(giStr, ".agents/ycode.json") {
		t.Error(".gitignore should contain .agents/ycode.json")
	}
	if !strings.Contains(giStr, ".agents/ycode/settings.local.json") {
		t.Error(".gitignore should contain .agents/ycode/settings.local.json")
	}
	if !strings.Contains(giStr, ".agents/ycode/sessions/") {
		t.Error(".gitignore should contain .agents/ycode/sessions/")
	}
	if !strings.Contains(giStr, ".agents/ycode/cache/") {
		t.Error(".gitignore should contain .agents/ycode/cache/")
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

	// Verify AGENTS.md was created with proper content.
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	agentsStr := string(agents)
	if !strings.Contains(agentsStr, "AI coding assistants") {
		t.Error("AGENTS.md should reference AI coding assistants")
	}
	if !strings.Contains(agentsStr, "USAGE.md") {
		t.Error("AGENTS.md should reference USAGE.md")
	}
}

func TestInitializeRepoCreatesGitRepoWarning(t *testing.T) {
	root := t.TempDir()

	// Don't initialize git - should produce warning
	report, err := InitializeRepo(root)
	if err != nil {
		t.Fatalf("InitializeRepo failed: %v", err)
	}

	if len(report.Warnings) == 0 {
		t.Error("should produce warning for missing git repository")
	}

	foundGitWarning := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "git") {
			foundGitWarning = true
			break
		}
	}
	if !foundGitWarning {
		t.Error("warning should mention git")
	}
}

func TestInitializeRepoDetectsExistingGit(t *testing.T) {
	root := t.TempDir()

	// Initialize git repo
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)

	report, err := InitializeRepo(root)
	if err != nil {
		t.Fatalf("InitializeRepo failed: %v", err)
	}

	if report.GitStatus != "initialized" {
		t.Errorf("expected git status 'initialized', got %q", report.GitStatus)
	}

	if len(report.Warnings) > 0 {
		t.Error("should not produce warnings for initialized git repo")
	}
}

func TestInitializeRepoIsIdempotent(t *testing.T) {
	root := t.TempDir()

	// Pre-create CLAUDE.md with custom content.
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("custom guidance\n"), 0o644)
	// Pre-create AGENTS.md with custom content.
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("custom agents guidance\n"), 0o644)
	// Pre-create .gitignore with one entry already present.
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".agents/ycode/settings.local.json\n"), 0o644)

	first, err := InitializeRepo(root)
	if err != nil {
		t.Fatalf("first init failed: %v", err)
	}
	firstRendered := first.Render()
	if !strings.Contains(firstRendered, "CLAUDE.md") || !strings.Contains(firstRendered, "skipped (already exists)") {
		t.Error("first init should skip existing CLAUDE.md")
	}
	if !strings.Contains(firstRendered, "AGENTS.md") || !strings.Contains(firstRendered, "skipped (already exists)") {
		t.Error("first init should skip existing AGENTS.md")
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

	// Verify AGENTS.md was NOT overwritten.
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(agents) != "custom agents guidance\n" {
		t.Error("AGENTS.md should not be overwritten")
	}

	// Verify .gitignore doesn't duplicate entries.
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	giStr := string(gi)
	if strings.Count(giStr, ".agents/ycode/settings.local.json") != 1 {
		t.Error(".gitignore should not have duplicate entries")
	}
}

func TestRenderInitClaudeMDDetectsGoProject(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644)

	detection := detectRepo(root)
	metadata := buildProjectMetadata(root, &detection)
	md := RenderInitClaudeMD(root, metadata)
	if !strings.Contains(md, "Go") {
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

	detection := detectRepo(root)
	metadata := buildProjectMetadata(root, &detection)
	md := RenderInitClaudeMD(root, metadata)
	if !strings.Contains(md, "Rust") {
		t.Errorf("should detect Rust, got:\n%s", md)
	}
	if !strings.Contains(md, "cargo") {
		t.Error("should include Rust verification commands")
	}
}

func TestRenderInitClaudeMDDetectsPythonAndNextJS(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"demo\"\n"), 0o644)
	os.WriteFile(filepath.Join(root, "package.json"),
		[]byte(`{"dependencies":{"next":"14.0.0","react":"18.0.0"},"devDependencies":{"typescript":"5.0.0"}}`),
		0o644)

	detection := detectRepo(root)
	metadata := buildProjectMetadata(root, &detection)
	md := RenderInitClaudeMD(root, metadata)
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
}

func TestRenderInitClaudeMDNoLanguagesDetected(t *testing.T) {
	root := t.TempDir()

	detection := detectRepo(root)
	metadata := buildProjectMetadata(root, &detection)
	md := RenderInitClaudeMD(root, metadata)
	if !strings.Contains(md, "No specific language markers") {
		t.Error("should report no languages detected")
	}
	if !strings.Contains(md, "none detected") {
		t.Error("should report no frameworks detected")
	}
}

func TestRenderInitAgentsMD(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "cmd"), 0o755)

	detection := detectRepo(root)
	metadata := buildProjectMetadata(root, &detection)
	md := RenderInitAgentsMD(root, metadata)

	if !strings.Contains(md, "AGENTS.md") {
		t.Error("should have AGENTS.md header")
	}
	if !strings.Contains(md, "AI coding assistants") {
		t.Error("should reference AI coding assistants")
	}
	if !strings.Contains(md, "USAGE.md") {
		t.Error("should reference USAGE.md")
	}
	// Should NOT reference CLAUDE.md when it doesn't exist.
	if strings.Contains(md, "CLAUDE.md") {
		t.Error("should not reference CLAUDE.md when it doesn't exist")
	}
}

func TestRenderInitAgentsMDWithClaudeMD(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644)
	// Create CLAUDE.md so the reference is included.
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# CLAUDE.md\n"), 0o644)

	detection := detectRepo(root)
	metadata := buildProjectMetadata(root, &detection)
	md := RenderInitAgentsMD(root, metadata)

	if !strings.Contains(md, "CLAUDE.md") {
		t.Error("should reference CLAUDE.md when it exists")
	}
	if !strings.Contains(md, "USAGE.md") {
		t.Error("should still reference USAGE.md")
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

func TestDetectBuildCommand(t *testing.T) {
	root := t.TempDir()

	// Test Go project
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0o644)
	d := detectRepo(root)
	metadata := buildProjectMetadata(root, &d)
	if metadata.BuildCmd != "go build ./..." {
		t.Errorf("expected 'go build ./...', got %q", metadata.BuildCmd)
	}
}

func TestDetectNodeScripts(t *testing.T) {
	root := t.TempDir()

	// Test Node project with build script
	packageJSON := `{"scripts":{"build":"next build","test":"jest","lint":"eslint ."}}`
	os.WriteFile(filepath.Join(root, "package.json"), []byte(packageJSON), 0o644)

	d := detectRepo(root)
	metadata := buildProjectMetadata(root, &d)

	if !strings.Contains(metadata.BuildCmd, "run build") {
		t.Errorf("expected build command with 'run build', got %q", metadata.BuildCmd)
	}
	if !strings.Contains(metadata.TestCmd, "run test") {
		t.Errorf("expected test command with 'run test', got %q", metadata.TestCmd)
	}
	if !strings.Contains(metadata.LintCmd, "run lint") {
		t.Errorf("expected lint command with 'run lint', got %q", metadata.LintCmd)
	}
}

func TestDetectPackageManager(t *testing.T) {
	root := t.TempDir()

	// Test pnpm
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{}}`), 0o644)
	os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte(""), 0o644)

	d := detectRepo(root)
	metadata := buildProjectMetadata(root, &d)

	if metadata.PackageMgr != "pnpm" {
		t.Errorf("expected package manager 'pnpm', got %q", metadata.PackageMgr)
	}
}
