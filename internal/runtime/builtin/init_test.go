package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGenerator_GatherContext(t *testing.T) {
	// Create temp directory with test files.
	tmpDir := t.TempDir()

	// Write README.md.
	readme := "# Test Project\n\nThis is a test project."
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write go.mod.
	gomod := "module github.com/test/project\n\ngo 1.21"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create InitGenerator.
	gen := NewInitGenerator(tmpDir)

	// Test Generate.
	result, err := gen.Generate("")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify files were read.
	if len(result.FilesRead) == 0 {
		t.Error("expected FilesRead to be non-empty")
	}

	// Verify prompt was generated.
	if result.Content == "" {
		t.Error("expected non-empty prompt content")
	}

	// Verify template variables were substituted.
	if strings.Contains(result.Content, "{{ARGS}}") {
		t.Error("template variable {{ARGS}} was not substituted")
	}
	if strings.Contains(result.Content, "{{PATH}}") {
		t.Error("template variable {{PATH}} was not substituted")
	}
}

func TestInitGenerator_WithArgs(t *testing.T) {
	tmpDir := t.TempDir()

	// Write README.md.
	readme := "# Test Project"
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := NewInitGenerator(tmpDir)
	result, err := gen.Generate("backend focus")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify args were substituted.
	if !strings.Contains(result.Content, "backend focus") {
		t.Error("expected args to be substituted in prompt")
	}
}

func TestInitGenerator_IdentifyQuestions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create generator with no files (should have questions).
	gen := NewInitGenerator(tmpDir)
	result, err := gen.Generate("")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should have questions about missing README and build commands.
	if len(result.Questions) == 0 {
		t.Error("expected questions for empty project")
	}

	// Check for expected questions.
	hasProjectNameQuestion := false
	for _, q := range result.Questions {
		if strings.Contains(q, "name") || strings.Contains(q, "purpose") {
			hasProjectNameQuestion = true
			break
		}
	}
	if !hasProjectNameQuestion {
		t.Error("expected question about project name/purpose")
	}
}

func TestInitGenerator_GoProject(t *testing.T) {
	tmpDir := t.TempDir()

	// Write go.mod.
	gomod := "module github.com/test/project\n\ngo 1.21"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := NewInitGenerator(tmpDir)
	result, err := gen.Generate("")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should detect Go language.
	foundGo := false
	for _, lang := range result.FilesRead {
		if lang == "go.mod" {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Error("expected go.mod to be read")
	}

	// Should have fewer questions (build commands are known).
	for _, q := range result.Questions {
		if strings.Contains(q, "build command") {
			t.Error("should not ask about build command for Go projects")
		}
	}
}

func TestInitGenerator_NPMProject(t *testing.T) {
	tmpDir := t.TempDir()

	// Write package.json.
	pkgJSON := `{
		"name": "test-project",
		"scripts": {
			"build": "tsc",
			"test": "jest",
			"lint": "eslint"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := NewInitGenerator(tmpDir)
	result, err := gen.Generate("")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify package.json was read.
	foundPkg := false
	for _, f := range result.FilesRead {
		if f == "package.json" {
			foundPkg = true
			break
		}
	}
	if !foundPkg {
		t.Error("expected package.json to be read")
	}
}

func TestInitGenerator_WithCI(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .github/workflows directory.
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write CI config.
	ci := `name: CI
on: [push]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - run: go test ./...
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "ci.yml"), []byte(ci), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write README.
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := NewInitGenerator(tmpDir)
	result, err := gen.Generate("")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify CI file was read.
	foundCI := false
	for _, f := range result.FilesRead {
		if strings.Contains(f, "ci.yml") {
			foundCI = true
			break
		}
	}
	if !foundCI {
		t.Error("expected CI config to be read")
	}
}

func TestExtractProjectName(t *testing.T) {
	gen := &InitGenerator{}

	tests := []struct {
		name     string
		content  string
		filename string
		expected string
	}{
		{
			name:     "h1 title",
			content:  "# My Project\n\nDescription here.",
			filename: "README.md",
			expected: "My Project",
		},
		{
			name:     "first line fallback",
			content:  "My Project\n\nDescription here.",
			filename: "README.txt",
			expected: "My Project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.extractProjectName(tt.content, tt.filename)
			if got != tt.expected {
				t.Errorf("extractProjectName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDetectJSLanguage(t *testing.T) {
	gen := &InitGenerator{}

	if lang := gen.detectJSLanguage(`{"name": "test"}`); lang != "JavaScript" {
		t.Errorf("expected JavaScript, got %s", lang)
	}

	if lang := gen.detectJSLanguage(`{"devDependencies": {"typescript": "^5.0"}}`); lang != "TypeScript" {
		t.Errorf("expected TypeScript, got %s", lang)
	}
}

func TestExtractNPMScripts(t *testing.T) {
	gen := &InitGenerator{}

	pkgJSON := `{
		"scripts": {
			"build": "tsc",
			"test": "jest",
			"lint": "eslint",
			"dev": "vite"
		}
	}`

	scripts := gen.extractNPMScripts(pkgJSON)

	if scripts["build"] != "npm run build" {
		t.Errorf("expected build='npm run build', got %q", scripts["build"])
	}
	if scripts["test"] != "npm test" {
		t.Errorf("expected test='npm test', got %q", scripts["test"])
	}
	if scripts["lint"] != "npm run lint" {
		t.Errorf("expected lint='npm run lint', got %q", scripts["lint"])
	}
}

func TestExtractMakeTargets(t *testing.T) {
	gen := &InitGenerator{}

	makefile := `
build:
	go build

test:
	go test

lint:
	golangci-lint run
`

	targets := gen.extractMakeTargets(makefile)

	if targets["build"] != "build" {
		t.Errorf("expected build target, got %q", targets["build"])
	}
	if targets["test"] != "test" {
		t.Errorf("expected test target, got %q", targets["test"])
	}
	if targets["lint"] != "lint" {
		t.Errorf("expected lint target, got %q", targets["lint"])
	}
}

func TestTruncateContent(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"

	// Test truncation.
	truncated := truncateContent(content, 3)
	if !strings.Contains(truncated, "line1") {
		t.Error("expected truncated content to contain line1")
	}
	if strings.Contains(truncated, "line5") {
		t.Error("expected truncated content to NOT contain line5")
	}
	if !strings.Contains(truncated, "truncated") {
		t.Error("expected truncated content to contain truncation marker")
	}

	// Test no truncation needed.
	notTruncated := truncateContent(content, 10)
	if notTruncated != content {
		t.Error("expected no truncation when content is within limit")
	}
}
