package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuggestSimilarFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some files.
	for _, name := range []string{"main.go", "main_test.go", "handler.go", "config.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Ask for a file that doesn't exist but is similar.
	suggestion := suggestSimilarFiles(filepath.Join(dir, "mian.go"))
	if suggestion == "" {
		t.Fatal("expected suggestions, got empty string")
	}
	if !strings.Contains(suggestion, "main.go") {
		t.Errorf("expected main.go in suggestions, got: %s", suggestion)
	}
}

func TestSuggestSimilarFiles_NoMatch(t *testing.T) {
	dir := t.TempDir()

	// Create a file with no similarity.
	if err := os.WriteFile(filepath.Join(dir, "totally_different.rs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestion := suggestSimilarFiles(filepath.Join(dir, "xyz.go"))
	// May or may not suggest depending on scoring — just verify no panic.
	_ = suggestion
}

func TestSuggestSimilarFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	suggestion := suggestSimilarFiles(filepath.Join(dir, "missing.txt"))
	if suggestion != "" {
		t.Errorf("expected empty suggestion for empty dir, got: %s", suggestion)
	}
}

func TestSuggestSimilarFiles_NonexistentDir(t *testing.T) {
	suggestion := suggestSimilarFiles("/nonexistent/path/file.txt")
	if suggestion != "" {
		t.Errorf("expected empty suggestion for nonexistent dir, got: %s", suggestion)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"main.go", "mian.go", 2},
	}
	for _, tc := range tests {
		got := levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
