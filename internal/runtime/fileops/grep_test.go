package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrepSearch_Basic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "func main",
		Path:       dir,
		OutputMode: GrepOutputContent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if result.Matches[0].Line != 3 {
		t.Errorf("expected line 3, got %d", result.Matches[0].Line)
	}
	if result.Matches[0].IsContext {
		t.Error("match line should not be marked as context")
	}
}

func TestGrepSearch_FilesWithMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("no match\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "package",
		Path:       dir,
		OutputMode: GrepOutputFilesWithMatches,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(result.Files), result.Files)
	}
}

func TestGrepSearch_ContextLines(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nMATCH\nline5\nline6\nline7\n"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "MATCH",
		Path:       dir,
		OutputMode: GrepOutputContent,
		Context:    2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should get lines 2-6 (2 before MATCH at line 4, MATCH, 2 after).
	if len(result.Matches) != 5 {
		t.Fatalf("expected 5 lines (2 before + match + 2 after), got %d", len(result.Matches))
	}

	// Verify context vs match flags.
	for _, m := range result.Matches {
		if m.Content == "MATCH" {
			if m.IsContext {
				t.Error("MATCH line should not be context")
			}
			if m.Line != 4 {
				t.Errorf("MATCH should be line 4, got %d", m.Line)
			}
		} else {
			if !m.IsContext {
				t.Errorf("line %d (%q) should be context", m.Line, m.Content)
			}
		}
	}
}

func TestGrepSearch_ContextMerging(t *testing.T) {
	dir := t.TempDir()
	// Two matches 2 lines apart — with context=1 their windows overlap.
	content := "a\nMATCH1\nb\nMATCH2\nc\n"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "MATCH",
		Path:       dir,
		OutputMode: GrepOutputContent,
		Context:    1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Window: line 1-3 (MATCH1 context) merges with line 3-5 (MATCH2 context) => lines 1-5.
	if len(result.Matches) != 5 {
		t.Fatalf("expected 5 merged lines, got %d", len(result.Matches))
	}

	// "b" on line 3 should appear exactly once (merged).
	bCount := 0
	for _, m := range result.Matches {
		if m.Content == "b" {
			bCount++
		}
	}
	if bCount != 1 {
		t.Errorf("expected 'b' to appear once (merged), got %d", bCount)
	}
}

func TestGrepSearch_BeforeAfter(t *testing.T) {
	dir := t.TempDir()
	content := "a\nb\nc\nMATCH\nd\ne\nf\n"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "MATCH",
		Path:       dir,
		OutputMode: GrepOutputContent,
		Before:     1,
		After:      3,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1 before (c) + MATCH + 3 after (d, e, f) = 5 lines.
	if len(result.Matches) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(result.Matches))
	}
	if result.Matches[0].Content != "c" {
		t.Errorf("first line should be 'c', got %q", result.Matches[0].Content)
	}
}

func TestGrepSearch_IgnoreCase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("Hello World\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "hello",
		Path:       dir,
		OutputMode: GrepOutputContent,
		IgnoreCase: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match with case-insensitive, got %d", len(result.Matches))
	}
}

func TestGrepSearch_TypeFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "script.py"), []byte("# package\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "package",
		Path:       dir,
		OutputMode: GrepOutputFilesWithMatches,
		Type:       "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 go file, got %d", len(result.Files))
	}
}

func TestGrepSearch_Offset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("a1\na2\na3\na4\na5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "a",
		Path:       dir,
		OutputMode: GrepOutputContent,
		HeadLimit:  2,
		Offset:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches (offset 2, limit 2), got %d", len(result.Matches))
	}
	if result.Matches[0].Content != "a3" {
		t.Errorf("expected first match to be 'a3', got %q", result.Matches[0].Content)
	}
	if result.Matches[1].Content != "a4" {
		t.Errorf("expected second match to be 'a4', got %q", result.Matches[1].Content)
	}
}

func TestGrepSearch_Count(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("a\na\nb\na\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GrepSearch(GrepParams{
		Pattern:    "a",
		Path:       dir,
		OutputMode: GrepOutputCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 3 {
		t.Errorf("expected count 3, got %d", result.Count)
	}
}
