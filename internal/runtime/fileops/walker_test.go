package fileops

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestShouldSkipDir(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{".git", true},
		{"node_modules", true},
		{"vendor", true},
		{"__pycache__", true},
		{"dist", true},
		{"priorart", true},
		{".hidden", true},
		{"src", false},
		{"internal", false},
		{"cmd", false},
		{".", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSkipDir(tt.name); got != tt.want {
				t.Errorf("ShouldSkipDir(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestWalkSourceFiles(t *testing.T) {
	dir := t.TempDir()

	// Create directory structure.
	dirs := []string{
		"src",
		"src/pkg",
		".git",
		".git/objects",
		"node_modules",
		"node_modules/foo",
		"vendor",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create files.
	files := map[string]string{
		"src/main.go":            "package main",
		"src/pkg/util.go":        "package pkg",
		"README.md":              "# readme",
		".git/config":            "[core]",
		"node_modules/foo/index": "module.exports",
		"vendor/lib.go":          "package vendor",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Walk and collect visited files.
	var visited []string
	err := WalkSourceFiles(dir, nil, func(path string, d fs.DirEntry) error {
		rel, _ := filepath.Rel(dir, path)
		visited = append(visited, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(visited)

	// Should include src files and README, but not .git, node_modules, vendor.
	want := []string{"README.md", "src/main.go", "src/pkg/util.go"}
	sort.Strings(want)

	if len(visited) != len(want) {
		t.Fatalf("visited %d files %v, want %d files %v", len(visited), visited, len(want), want)
	}
	for i, v := range visited {
		if v != want[i] {
			t.Errorf("visited[%d] = %q, want %q", i, v, want[i])
		}
	}
}

func TestWalkSourceFiles_MaxFileSize(t *testing.T) {
	dir := t.TempDir()

	// Create a small and large file.
	if err := os.WriteFile(filepath.Join(dir, "small.go"), []byte("package small"), 0o644); err != nil {
		t.Fatal(err)
	}
	large := make([]byte, 2<<20) // 2MB
	if err := os.WriteFile(filepath.Join(dir, "large.go"), large, 0o644); err != nil {
		t.Fatal(err)
	}

	var visited []string
	opts := &WalkOptions{MaxFileSize: 1 << 20}
	err := WalkSourceFiles(dir, opts, func(path string, d fs.DirEntry) error {
		rel, _ := filepath.Rel(dir, path)
		visited = append(visited, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(visited) != 1 || visited[0] != "small.go" {
		t.Errorf("expected only small.go, got %v", visited)
	}
}

func TestWalkSourceFiles_IgnoreFile(t *testing.T) {
	dir := t.TempDir()

	// Create .gitignore that ignores *.log files.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "debug.log"), []byte("log data"), 0o644); err != nil {
		t.Fatal(err)
	}

	var visited []string
	err := WalkSourceFiles(dir, nil, func(path string, d fs.DirEntry) error {
		rel, _ := filepath.Rel(dir, path)
		visited = append(visited, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// .gitignore itself should be visited, but debug.log should not.
	for _, v := range visited {
		if v == "debug.log" {
			t.Error("debug.log should have been ignored via .gitignore")
		}
	}
	found := false
	for _, v := range visited {
		if v == "app.go" {
			found = true
		}
	}
	if !found {
		t.Error("app.go should have been visited")
	}
}

func TestIsSourceExt(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".go", true},
		{".Go", true},
		{".py", true},
		{".rs", true},
		{".exe", false},
		{".dll", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsSourceExt(tt.ext); got != tt.want {
			t.Errorf("IsSourceExt(%q) = %v, want %v", tt.ext, got, tt.want)
		}
	}
}
