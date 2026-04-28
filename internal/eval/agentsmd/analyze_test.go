package agentsmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/builtin"
)

func TestDetectBoilerplate(t *testing.T) {
	content := `# Project

## Conventions
- prefer small, reviewable changes
- write clean code at all times
- follow best practices for testing
- never modify files under priorart/
- keep functions short and focused
`
	lines := splitLines(content)
	findings := detectBoilerplate(lines)

	// Should detect 4 boilerplate lines, not the guardrail or heading.
	if len(findings) != 4 {
		t.Errorf("expected 4 boilerplate findings, got %d", len(findings))
		for _, f := range findings {
			t.Logf("  L%d: %s", f.Line, f.Text)
		}
	}

	// Verify guardrail line was NOT flagged as boilerplate.
	for _, f := range findings {
		if f.Line == 7 { // "never modify files under priorart/" is line 7
			t.Error("guardrail line 'never modify files under priorart/' should not be flagged as boilerplate")
		}
	}
}

func TestDetectGuardrails(t *testing.T) {
	content := `# Rules
- Never modify files under priorart/
- Do not use bare ./... in go commands
- Always run make build before committing
- This is a normal line
- Keep shared defaults in config
- Must not commit code with vet warnings
`
	lines := splitLines(content)
	findings := detectGuardrails(lines)

	if len(findings) != 4 {
		t.Errorf("expected 4 guardrail findings, got %d", len(findings))
		for _, f := range findings {
			t.Logf("  L%d: %s", f.Line, f.Text)
		}
	}
}

func TestDetectGuardrails_SkipsCodeBlocks(t *testing.T) {
	content := "# Build\n```bash\nnever use ./...\n```\n- Never modify priorart/\n"
	lines := splitLines(content)
	findings := detectGuardrails(lines)

	if len(findings) != 1 {
		t.Errorf("expected 1 guardrail (skipping code block), got %d", len(findings))
	}
}

func TestCountCodeBlocks(t *testing.T) {
	content := "text\n```bash\nmake build\n```\nmore\n```\ngo test\n```\n"
	lines := splitLines(content)
	count := countCodeBlocks(lines)
	if count != 2 {
		t.Errorf("expected 2 code blocks, got %d", count)
	}
}

func TestCheckPathReferences(t *testing.T) {
	dir := t.TempDir()
	// Create some files.
	os.MkdirAll(filepath.Join(dir, "internal", "api"), 0o755)
	os.WriteFile(filepath.Join(dir, "internal", "api", "provider.go"), []byte("package api"), 0o644)

	content := `Architecture:
- Providers: internal/api/provider.go
- Tools: internal/tools/registry.go (does not exist)
- Docs: docs/missing.md
`
	lines := splitLines(content)
	findings := checkPathReferences(lines, dir)

	if len(findings) != 2 {
		t.Errorf("expected 2 broken paths, got %d", len(findings))
		for _, f := range findings {
			t.Logf("  %s: %s", f.Text, f.Detail)
		}
	}
}

func TestParseMakefileTargets(t *testing.T) {
	dir := t.TempDir()
	makefile := `VERSION ?= dev
PACKAGES := $(shell go list ./...)

.PHONY: help init build test compile deploy validate clean

help: ## Show help
	@echo help

build: ## Full quality gate
	@./scripts/build.sh

test: ## Run tests
	go test ./...

compile: ## Compile
	go build ./cmd/app/

custom-target:
	echo custom
`
	path := filepath.Join(dir, "Makefile")
	os.WriteFile(path, []byte(makefile), 0o644)

	targets, err := ParseMakefileTargets(path)
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"help", "init", "build", "test", "compile", "deploy", "validate", "clean", "custom-target"}
	for _, target := range expected {
		if !targets[target] {
			t.Errorf("expected target %q not found", target)
		}
	}

	if targets["VERSION"] {
		t.Error("variable assignment should not be parsed as target")
	}
}

func TestCheckMakeTargets(t *testing.T) {
	targets := map[string]bool{
		"build":   true,
		"test":    true,
		"compile": true,
	}

	content := `## Build Commands
- make build
- make test
- make nonexistent-target
- make also-missing
`
	lines := splitLines(content)
	findings := checkMakeTargets(lines, targets)

	if len(findings) != 2 {
		t.Errorf("expected 2 broken commands, got %d", len(findings))
		for _, f := range findings {
			t.Logf("  L%d: %s — %s", f.Line, f.Text, f.Detail)
		}
	}
}

func TestComputeScore(t *testing.T) {
	// A good report should score high.
	good := &Report{
		TotalLines:       100,
		CommandDensity:   0.08,
		GuardrailDensity: 0.06,
		BoilerplateRatio: 0.0,
		PathAccuracy:     1.0,
		CommandAccuracy:  1.0,
	}
	score := ComputeScore(good)
	if score < 0.7 {
		t.Errorf("good report should score > 0.7, got %.2f", score)
	}

	// A bad report should score low.
	bad := &Report{
		TotalLines:       100,
		CommandDensity:   0.0,
		GuardrailDensity: 0.0,
		BoilerplateRatio: 0.15,
		PathAccuracy:     0.5,
		CommandAccuracy:  0.5,
	}
	badScore := ComputeScore(bad)
	if badScore > 0.4 {
		t.Errorf("bad report should score < 0.4, got %.2f", badScore)
	}

	if badScore >= score {
		t.Error("good report should outscore bad report")
	}
}

func TestFormatComparison(t *testing.T) {
	entries := []ComparisonEntry{
		{
			Name: "tool-a",
			Report: &Report{
				TotalLines:       50,
				Score:            0.8,
				CommandDensity:   0.06,
				GuardrailDensity: 0.04,
				PathAccuracy:     1.0,
				CommandAccuracy:  1.0,
				CodeBlocks:       3,
				Sections:         []string{"Build", "Test"},
			},
		},
		{
			Name: "tool-b",
			Report: &Report{
				TotalLines:       100,
				Score:            0.5,
				CommandDensity:   0.02,
				GuardrailDensity: 0.01,
				BoilerplateRatio: 0.1,
				PathAccuracy:     0.8,
				CommandAccuracy:  0.9,
				CodeBlocks:       1,
				Sections:         []string{"Intro"},
				Boilerplate:      []Finding{{Line: 5, Text: "write clean code", Kind: "boilerplate", Detail: "generic"}},
			},
		},
	}

	output := FormatComparison(entries)

	// Should contain both tool names and table structure.
	if !contains(output, "tool-a") || !contains(output, "tool-b") {
		t.Error("output should contain both tool names")
	}
	if !contains(output, "Score (0-10)") {
		t.Error("output should contain Score header")
	}
	if !contains(output, "Boilerplate") {
		t.Error("output should contain findings section")
	}
}

func TestAnalyze_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Find repo root by looking for go.mod.
	root := findRepoRoot(t)
	agentsPath := filepath.Join(root, "AGENTS.md")
	content, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Skipf("AGENTS.md not found: %v", err)
	}

	r := Analyze(string(content), Options{ProjectRoot: root})

	t.Logf("Score: %.2f (%.1f/10)", r.Score, r.Score*10)
	t.Logf("Lines: %d, Code blocks: %d, Sections: %d", r.TotalLines, r.CodeBlocks, len(r.Sections))
	t.Logf("Command density: %.1f%%, Guardrail density: %.1f%%", r.CommandDensity*100, r.GuardrailDensity*100)
	t.Logf("Boilerplate: %d, Broken paths: %d, Broken commands: %d", len(r.Boilerplate), len(r.BrokenPaths), len(r.BrokenCommands))
	for _, f := range r.BrokenPaths {
		t.Logf("  broken path L%d: %s", f.Line, f.Text)
	}
	for _, f := range r.BrokenCommands {
		t.Logf("  broken cmd L%d: %s — %s", f.Line, f.Text, f.Detail)
	}
	for _, f := range r.Contradictions {
		t.Logf("  contradiction: %s", f.Detail)
	}

	if r.Score < 0.3 {
		t.Errorf("AGENTS.md score %.2f is below minimum threshold 0.3", r.Score)
	}
	if r.TotalLines == 0 {
		t.Error("AGENTS.md appears empty")
	}
}

func TestBenchAllTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	root := findRepoRoot(t)

	tools := []struct {
		name string
		path string
		root string
	}{
		{"ycode", filepath.Join(root, "AGENTS.md"), root},
		{"opencode", filepath.Join(root, "priorart/opencode/AGENTS.md"), filepath.Join(root, "priorart/opencode")},
		{"clawcode", filepath.Join(root, "priorart/clawcode/CLAUDE.md"), filepath.Join(root, "priorart/clawcode")},
		{"gemini-cli", filepath.Join(root, "priorart/geminicli/GEMINI.md"), filepath.Join(root, "priorart/geminicli")},
		{"codex", filepath.Join(root, "priorart/codex/AGENTS.md"), filepath.Join(root, "priorart/codex")},
		{"aider", filepath.Join(root, "priorart/aider/CONTRIBUTING.md"), filepath.Join(root, "priorart/aider")},
	}

	var entries []ComparisonEntry
	for _, tool := range tools {
		content, err := os.ReadFile(tool.path)
		if err != nil {
			t.Logf("skip %s: %v", tool.name, err)
			continue
		}
		r := Analyze(string(content), Options{ProjectRoot: tool.root})
		entries = append(entries, ComparisonEntry{Name: tool.name, Report: r})
	}

	t.Log("\n" + FormatComparison(entries))

	// Also mine git conventions for the ycode repo.
	conventions, err := MineCommitConventions(root, 200)
	if err != nil {
		t.Logf("git mining: %v", err)
	} else {
		t.Log("\n" + FormatConventions(conventions))
	}
}

// TestInitGeneration runs ycode's InitGenerator against diverse priorart repos
// and compares the generated output quality against each repo's existing
// instruction file. This is the "real test" — fresh generation vs hand-written.
func TestInitGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping init generation test in short mode")
	}

	root := findRepoRoot(t)

	// Diverse repos: different languages, sizes, with/without existing instruction files.
	repos := []struct {
		name     string
		dir      string
		existing string // path to existing instruction file, empty if none
		lang     string
	}{
		{"opencode", "priorart/opencode", "priorart/opencode/AGENTS.md", "TypeScript"},
		{"clawcode", "priorart/clawcode", "priorart/clawcode/CLAUDE.md", "Rust"},
		{"codex", "priorart/codex", "priorart/codex/AGENTS.md", "TypeScript/Rust"},
		{"aider", "priorart/aider", "", "Python"},
		{"gemini-cli", "priorart/geminicli", "", "TypeScript"},
		{"openhands", "priorart/openhands", "priorart/openhands/AGENTS.md", "Python"},
	}

	var results []string
	results = append(results, "## Init Generation Benchmark\n")
	results = append(results, "Runs ycode's InitGenerator on repos it has never seen, then scores the output.\n")
	results = append(results, "| Repo | Lang | Generated Score | Existing Score | Delta | Generated Lines | Existing Lines |")
	results = append(results, "|------|------|----------------|---------------|-------|----------------|---------------|")

	for _, repo := range repos {
		repoDir := filepath.Join(root, repo.dir)
		if _, err := os.Stat(repoDir); err != nil {
			t.Logf("skip %s: %v", repo.name, err)
			continue
		}

		// Run ycode's InitGenerator.
		gen := builtin.NewInitGenerator(repoDir)
		result, err := gen.Generate("")
		if err != nil {
			t.Logf("skip %s: init error: %v", repo.name, err)
			continue
		}

		// The generated content is the UserPrompt — but we need the LLM output.
		// Since we can't call an LLM in a unit test, we analyze the *prompt quality*
		// and the *existing file quality* as proxies.
		// Instead, let's analyze what the generator gathered and score the prompt.
		genReport := analyzeInitPrompt(result.UserPrompt, repoDir)

		// Score existing file if it exists.
		var existScore float64
		var existLines int
		existScoreStr := "n/a"
		existLinesStr := "n/a"
		if repo.existing != "" {
			existPath := filepath.Join(root, repo.existing)
			if content, err := os.ReadFile(existPath); err == nil {
				existReport := Analyze(string(content), Options{ProjectRoot: repoDir})
				existScore = existReport.Score
				existLines = existReport.TotalLines
				existScoreStr = fmt.Sprintf("%.1f", existScore*10)
				existLinesStr = fmt.Sprintf("%d", existLines)
			}
		}

		delta := ""
		if repo.existing != "" && existScoreStr != "n/a" {
			d := genReport.Score*10 - existScore*10
			if d > 0 {
				delta = fmt.Sprintf("+%.1f", d)
			} else {
				delta = fmt.Sprintf("%.1f", d)
			}
		}

		results = append(results, fmt.Sprintf("| %s | %s | %.1f | %s | %s | %d | %s |",
			repo.name, repo.lang, genReport.Score*10, existScoreStr, delta,
			genReport.TotalLines, existLinesStr))
	}

	t.Log("\n" + strings.Join(results, "\n"))
}

// analyzeInitPrompt evaluates the quality of context gathered by InitGenerator.
// Since we can't call an LLM in tests, we measure how much useful context
// was gathered (files read, commands found, sections covered).
func analyzeInitPrompt(prompt string, repoDir string) *Report {
	// The prompt contains gathered context with fenced code blocks,
	// file contents, and detected commands. Analyze it like an instruction file.
	return Analyze(prompt, Options{
		ProjectRoot:   repoDir,
		SkipPathCheck: true, // prompt contains template paths like {{PATH}}
	})
}

// --- helpers ---

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod in ancestors)")
		}
		dir = parent
	}
}
