package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/session"
)

func TestTruncateDiff_Small(t *testing.T) {
	diff := "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	got := truncateDiff(diff)
	if got != diff {
		t.Error("small diff should pass through unchanged")
	}
}

func TestTruncateDiff_Large(t *testing.T) {
	// Create a diff larger than maxDiffBytes.
	var b strings.Builder
	for b.Len() < maxDiffBytes+1000 {
		b.WriteString("+added line number something something something\n")
	}
	diff := b.String()

	got := truncateDiff(diff)
	if len(got) >= len(diff) {
		t.Error("large diff should be truncated")
	}
	if !strings.Contains(got, "[diff truncated:") {
		t.Error("truncated diff should contain truncation notice")
	}
}

func TestCleanCommitMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "feat: add login", "feat: add login"},
		{"fenced", "```\nfeat: add login\n```", "feat: add login"},
		{"fenced with lang", "```text\nfeat: add login\n```", "feat: add login"},
		{"quoted", `"feat: add login"`, "feat: add login"},
		{"whitespace", "  feat: add login  \n", "feat: add login"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanCommitMessage(tt.raw)
			if got != tt.want {
				t.Errorf("cleanCommitMessage(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestInferCommitType(t *testing.T) {
	tests := []struct {
		files []string
		want  string
	}{
		{[]string{"internal/auth/auth_test.go"}, "test"},
		{[]string{"README.md"}, "docs"},
		{[]string{"docs/architecture.md"}, "docs"},
		{[]string{"internal/api/client.go"}, "chore"},
		{[]string{"main.go", "main_test.go"}, "chore"}, // mixed = chore
	}

	for _, tt := range tests {
		got := inferCommitType(tt.files, nil)
		if got != tt.want {
			t.Errorf("inferCommitType(%v) = %q, want %q", tt.files, got, tt.want)
		}
	}
}

func TestTemplateFallback(t *testing.T) {
	gen := &CommitGenerator{workDir: "."}

	gc := &gitContext{
		stagedFiles: []string{"internal/api/client.go"},
	}
	got := gen.templateFallback(gc)
	if !strings.HasPrefix(got, "chore:") {
		t.Errorf("expected chore prefix, got %q", got)
	}
	if !strings.Contains(got, "client.go") {
		t.Errorf("single file should appear in message, got %q", got)
	}

	gc2 := &gitContext{
		modifiedFiles: []string{"a.go", "b.go", "c.go"},
	}
	got2 := gen.templateFallback(gc2)
	if !strings.Contains(got2, "3 files") {
		t.Errorf("expected '3 files', got %q", got2)
	}
}

func TestFormatResult(t *testing.T) {
	r := &CommitResult{
		Hash:    "abc1234",
		Message: "feat: add endpoint\n\nAdds the login endpoint.",
		Staged:  []string{"api.go", "auth.go"},
	}
	got := FormatResult(r)
	if !strings.Contains(got, "abc1234") {
		t.Error("should contain commit hash")
	}
	if !strings.Contains(got, "feat: add endpoint") {
		t.Error("should contain first line of message")
	}
	if !strings.Contains(got, "api.go") {
		t.Error("should list staged files")
	}
}

func TestFormatResult_HookError(t *testing.T) {
	r := &CommitResult{
		HookError: "lint check failed: unused variable",
	}
	got := FormatResult(r)
	if !strings.Contains(got, "pre-commit hook") {
		t.Error("should indicate hook failure")
	}
	if !strings.Contains(got, "lint check failed") {
		t.Error("should contain hook error output")
	}
}

// TestCommitGenerator_Generate_Integration tests the full workflow with a temp git repo.
func TestCommitGenerator_Generate_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a temp git repo.
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Test User")

	// Create an initial commit so git log works.
	writeFile(t, dir, "README.md", "# test\n")
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-m", "docs: initial commit")

	// Create a change to commit.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	provider := &mockProvider{response: "feat: add main entry point"}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "test-model"},
		},
	}
	gen := NewCommitGenerator(chain, dir)

	result, err := gen.Generate(context.Background(), &CommitRequest{
		FilesToStage: []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Hash == "" {
		t.Error("expected a commit hash")
	}
	if result.Message != "feat: add main entry point" {
		t.Errorf("unexpected message: %q", result.Message)
	}

	// Verify the commit exists in git log.
	out := mustOutput(t, dir, "git", "log", "--oneline", "-1")
	if !strings.Contains(out, "feat: add main entry point") {
		t.Errorf("commit message not in git log: %s", out)
	}
}

// TestCommitGenerator_DryRun ensures DryRun generates a message without committing.
func TestCommitGenerator_DryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Test User")
	writeFile(t, dir, "README.md", "# test\n")
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-m", "initial")

	writeFile(t, dir, "main.go", "package main\n")
	mustRun(t, dir, "git", "add", "main.go")

	provider := &mockProvider{response: "feat: add main"}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "test-model"},
		},
	}
	gen := NewCommitGenerator(chain, dir)

	result, err := gen.Generate(context.Background(), &CommitRequest{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Hash != "" {
		t.Error("DryRun should not produce a commit hash")
	}
	if result.Message == "" {
		t.Error("DryRun should still produce a message")
	}
}

// TestCommitGenerator_NoChanges verifies error when nothing to commit.
func TestCommitGenerator_NoChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Test User")
	writeFile(t, dir, "README.md", "# test\n")
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-m", "initial")

	provider := &mockProvider{response: "should not be called"}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "test-model"},
		},
	}
	gen := NewCommitGenerator(chain, dir)

	_, err := gen.Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for no changes")
	}
	if !strings.Contains(err.Error(), "no changes") {
		t.Errorf("expected 'no changes' error, got: %v", err)
	}
}

// TestCommitGenerator_LLMFallback verifies template fallback when LLM fails.
func TestCommitGenerator_LLMFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Test User")
	writeFile(t, dir, "README.md", "# test\n")
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-m", "initial")

	writeFile(t, dir, "main.go", "package main\n")

	provider := &mockProvider{err: fmt.Errorf("API error")}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "test-model"},
		},
	}
	gen := NewCommitGenerator(chain, dir)

	result, err := gen.Generate(context.Background(), &CommitRequest{
		FilesToStage: []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("should succeed with template fallback, got: %v", err)
	}
	if result.Hash == "" {
		t.Error("should have committed with fallback message")
	}
	if result.Message == "" {
		t.Error("should have a fallback message")
	}
}

// Helpers

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func mustOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
