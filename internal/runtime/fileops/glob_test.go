package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobSearch_Basic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GlobSearch(GlobParams{Pattern: "*.go", Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(result.Files), result.Files)
	}
}

func TestGlobSearch_DoubleStarRecursive(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure.
	nested := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		"main.go",
		filepath.Join("src", "app.go"),
		filepath.Join("src", "pkg", "util.go"),
		"readme.md",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := GlobSearch(GlobParams{Pattern: "**/*.go", Path: dir})
	if err != nil {
		t.Fatal(err)
	}

	// Should match all .go files at any depth.
	if len(result.Files) != 3 {
		t.Fatalf("expected 3 .go files, got %d: %v", len(result.Files), result.Files)
	}
}

func TestGlobSearch_DoubleStarMiddle(t *testing.T) {
	dir := t.TempDir()

	// Create src/cmd/main.go and src/pkg/main.go.
	for _, sub := range []string{"cmd", "pkg"} {
		d := filepath.Join(dir, "src", sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "main.go"), []byte("package "+sub), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Also create src/other.go (should NOT match src/**/main.go).
	if err := os.WriteFile(filepath.Join(dir, "src", "other.go"), []byte("package src"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GlobSearch(GlobParams{Pattern: "src/**/main.go", Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 main.go files, got %d: %v", len(result.Files), result.Files)
	}
}

func TestGlobSearch_SkipsDotDirs(t *testing.T) {
	dir := t.TempDir()

	// Create .git/config (should be skipped).
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GlobSearch(GlobParams{Pattern: "**/*", Path: dir})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range result.Files {
		rel, _ := filepath.Rel(dir, f)
		if filepath.Base(rel) == "config" {
			t.Error(".git/config should have been skipped")
		}
	}
}
