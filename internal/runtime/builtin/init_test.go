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
	gen := &InitGenerator{cwd: tmpDir}

	// Test gatherContext.
	ctx := gen.gatherContext()

	if !ctx.HasREADME {
		t.Error("expected HasREADME to be true")
	}
	if ctx.ProjectName != "This is a test project." {
		t.Errorf("expected ProjectName 'This is a test project.', got %q", ctx.ProjectName)
	}
	if ctx.BuildCmd != "go build ./..." {
		t.Errorf("expected BuildCmd 'go build ./...', got %q", ctx.BuildCmd)
	}
	if len(ctx.Languages) != 1 || ctx.Languages[0] != "Go" {
		t.Errorf("expected Languages ['Go'], got %v", ctx.Languages)
	}
}

func TestDetectLanguages(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with go.mod.
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	langs := detectLanguages(tmpDir)
	if len(langs) != 1 || langs[0] != "Go" {
		t.Errorf("expected ['Go'], got %v", langs)
	}
}

func TestExtractMakeTargets(t *testing.T) {
	makefile := `
build:
	go build

test:
	go test

lint:
	golangci-lint run
`
	build, test, lint := extractMakeTargets(makefile)

	if build != "make build" {
		t.Errorf("expected build='make build', got %q", build)
	}
	if test != "make test" {
		t.Errorf("expected test='make test', got %q", test)
	}
	if lint != "make lint" {
		t.Errorf("expected lint='make lint', got %q", lint)
	}
}

func TestExtractPkgJSONScripts(t *testing.T) {
	pkgJSON := `{
		"name": "test",
		"scripts": {
			"build": "tsc",
			"test": "jest",
			"lint": "eslint"
		}
	}`
	build, test, lint := extractPkgJSONScripts(pkgJSON)

	if build != "npm run build" {
		t.Errorf("expected build='npm run build', got %q", build)
	}
	if test != "npm test" {
		t.Errorf("expected test='npm test', got %q", test)
	}
	if lint != "npm run lint" {
		t.Errorf("expected lint='npm run lint', got %q", lint)
	}
}

func TestCleanInitOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain content",
			input:    "# AGENTS.md\n\nTest content.",
			expected: "# AGENTS.md\n\nTest content.",
		},
		{
			name:     "markdown fenced",
			input:    "```markdown\n# AGENTS.md\n\nTest content.\n```",
			expected: "# AGENTS.md\n\nTest content.",
		},
		{
			name:     "generic code fence",
			input:    "```\n# AGENTS.md\n\nTest content.\n```",
			expected: "# AGENTS.md\n\nTest content.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanInitOutput(tt.input)
			if got != tt.expected {
				t.Errorf("cleanInitOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestBuildInitPrompt(t *testing.T) {
	ctx := &initContext{
		ProjectName: "Test Project",
		Languages:   []string{"Go", "Python"},
		HasREADME:   true,
		HasUSAGE:    true,
		BuildCmd:    "make build",
		TestCmd:     "make test",
		Focus:       "backend focus",
	}

	prompt := buildInitPrompt(ctx)

	if !strings.Contains(prompt, "Test Project") {
		t.Error("expected prompt to contain project name")
	}
	if !strings.Contains(prompt, "Go, Python") {
		t.Error("expected prompt to contain languages")
	}
	if !strings.Contains(prompt, "backend focus") {
		t.Error("expected prompt to contain focus")
	}
	if !strings.Contains(prompt, "make build") {
		t.Error("expected prompt to contain build command")
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
