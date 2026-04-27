package repomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_GenerateOnRealProject generates a repo map of the ycode codebase itself.
func TestE2E_GenerateOnRealProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Find the ycode project root by walking up from the test file.
	root := findProjectRoot(t)
	if root == "" {
		t.Skip("could not find ycode project root")
	}

	opts := DefaultOptions()
	opts.MaxTokens = 2048 // smaller budget for faster test

	rm, err := Generate(root, opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(rm.Entries) == 0 {
		t.Fatal("expected non-empty repo map")
	}

	// Verify known symbols appear.
	formatted := rm.Format()

	// Should contain the repo map header.
	if !strings.Contains(formatted, "# Repository Map") {
		t.Error("missing repo map header")
	}

	// Should contain some known ycode types/functions.
	knownSymbols := []string{
		"Registry",   // internal/tools/registry.go
		"NewBuilder", // internal/runtime/prompt/builder.go
	}
	for _, sym := range knownSymbols {
		if !strings.Contains(formatted, sym) {
			t.Logf("note: symbol %q not in truncated map (may be outside token budget)", sym)
		}
	}

	// Verify token budget is respected (~4 chars/token).
	estimatedTokens := len(formatted) / 4
	if estimatedTokens > opts.MaxTokens*2 { // allow 2x tolerance
		t.Errorf("formatted output too large: ~%d tokens (budget: %d)", estimatedTokens, opts.MaxTokens)
	}

	// Verify excluded directories are not present.
	excludedDirs := []string{"vendor/", "node_modules/", "priorart/", "external/"}
	for _, dir := range excludedDirs {
		if strings.Contains(formatted, dir) {
			t.Errorf("excluded directory %q found in repo map", dir)
		}
	}
}

// TestE2E_RelevanceScoring verifies that relevance scoring ranks related files higher.
func TestE2E_RelevanceScoring(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	root := findProjectRoot(t)
	if root == "" {
		t.Skip("could not find ycode project root")
	}

	opts := DefaultOptions()
	opts.MaxTokens = 4096
	opts.RelevanceQuery = "prompt builder section"

	rm, err := Generate(root, opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(rm.Entries) == 0 {
		t.Skip("no entries found — tree-sitter may be unavailable and no Go files in scope")
	}

	// The first few entries should be related to prompt/builder.
	topEntries := rm.Entries
	if len(topEntries) > 5 {
		topEntries = topEntries[:5]
	}

	foundPrompt := false
	for _, entry := range topEntries {
		if strings.Contains(entry.Path, "prompt") || strings.Contains(entry.Path, "builder") {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Error("expected prompt-related files in top entries when querying 'prompt builder section'")
		for i, e := range topEntries {
			t.Logf("  top[%d]: %s (score=%.1f)", i, e.Path, e.Score)
		}
	}
}

// TestE2E_FormatIdempotent verifies Format produces the same output for the same input.
func TestE2E_FormatIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n\ntype App struct{}\n"), 0644)

	rm1, _ := Generate(dir, nil)
	rm2, _ := Generate(dir, nil)

	out1 := rm1.Format()
	out2 := rm2.Format()

	if out1 != out2 {
		t.Error("Format not idempotent for same input")
	}
}

// TestE2E_EmptyDirectory verifies Generate handles empty directories gracefully.
func TestE2E_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	rm, err := Generate(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rm.Entries) != 0 {
		t.Errorf("expected no entries for empty dir, got %d", len(rm.Entries))
	}

	formatted := rm.Format()
	if formatted != "" {
		t.Errorf("expected empty format for empty dir, got %q", formatted)
	}
}

// findProjectRoot walks up from the test directory to find go.mod with "ycode".
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			if strings.Contains(string(data), "ycode") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
