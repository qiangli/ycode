package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveImports_Simple(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extra.md"), []byte("Extra content"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := "# Main\n#import extra.md\n# End"
	visited := make(map[string]bool)
	result := ResolveImports(content, dir, visited, 0)

	if !strings.Contains(result, "Extra content") {
		t.Errorf("expected imported content, got:\n%s", result)
	}
	if !strings.Contains(result, "# Main") {
		t.Error("expected original content preserved")
	}
	if !strings.Contains(result, "# End") {
		t.Error("expected trailing content preserved")
	}
}

func TestResolveImports_Nested(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// sub/leaf.md
	if err := os.WriteFile(filepath.Join(sub, "leaf.md"), []byte("Leaf content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// sub/mid.md imports leaf.md
	if err := os.WriteFile(filepath.Join(sub, "mid.md"), []byte("#import leaf.md"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := "#import sub/mid.md"
	visited := make(map[string]bool)
	result := ResolveImports(content, dir, visited, 0)

	if !strings.Contains(result, "Leaf content") {
		t.Errorf("expected nested leaf content, got:\n%s", result)
	}
}

func TestResolveImports_CircularDetection(t *testing.T) {
	dir := t.TempDir()

	// a.md imports b.md, b.md imports a.md
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("#import b.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("#import a.md"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := "#import a.md"
	visited := make(map[string]bool)
	result := ResolveImports(content, dir, visited, 0)

	if !strings.Contains(result, "<!-- circular import: a.md -->") {
		t.Errorf("expected circular import marker, got:\n%s", result)
	}
}

func TestResolveImports_DepthLimit(t *testing.T) {
	dir := t.TempDir()

	// Create a chain: d0.md → d1.md → d2.md → d3.md → d4.md
	for i := 0; i < 5; i++ {
		var content string
		if i < 4 {
			content = "#import d" + string(rune('0'+i+1)) + ".md"
		} else {
			content = "Deep content"
		}
		name := "d" + string(rune('0'+i)) + ".md"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	content := "#import d0.md"
	visited := make(map[string]bool)
	result := ResolveImports(content, dir, visited, 0)

	// At depth 3 (MaxImportDepth), the #import d4.md line should be left as-is
	// (not resolved), because depth exceeded.
	if strings.Contains(result, "Deep content") {
		t.Errorf("expected depth limit to prevent resolving d4.md, got:\n%s", result)
	}
}

func TestResolveImports_MissingFile(t *testing.T) {
	dir := t.TempDir()
	content := "#import nonexistent.md"
	visited := make(map[string]bool)
	result := ResolveImports(content, dir, visited, 0)

	if !strings.Contains(result, "<!-- import not found: nonexistent.md -->") {
		t.Errorf("expected not-found marker, got:\n%s", result)
	}
}

func TestResolveImports_QuotedPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.md"), []byte("Quoted"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `#import "file.md"`
	visited := make(map[string]bool)
	result := ResolveImports(content, dir, visited, 0)

	if !strings.Contains(result, "Quoted") {
		t.Errorf("expected content from quoted import, got:\n%s", result)
	}
}

func TestResolveImports_NoDirectives(t *testing.T) {
	content := "# Normal content\nNo imports here.\n"
	visited := make(map[string]bool)
	result := ResolveImports(content, "/tmp", visited, 0)

	if !strings.Contains(result, "Normal content") {
		t.Error("expected original content preserved")
	}
}
