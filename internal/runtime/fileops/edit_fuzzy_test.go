package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFuzzyMatch_ExactMatch(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	search := "fmt.Println(\"hello\")"

	m := FindFuzzyMatch(content, search)
	if m == nil {
		t.Fatal("expected exact match")
	}
	if m.Level != 0 {
		t.Errorf("expected level 0, got %d", m.Level)
	}
}

func TestFindFuzzyMatch_LineTrimmed(t *testing.T) {
	// Content has trailing spaces that search does not.
	content := "func main() {  \n\tfmt.Println(\"hello\")  \n}  "
	search := "func main() {\n\tfmt.Println(\"hello\")\n}"

	m := FindFuzzyMatch(content, search)
	if m == nil {
		t.Fatal("expected line-trimmed match")
	}
	if m.Level != 1 {
		t.Errorf("expected level 1 (line-trimmed), got %d", m.Level)
	}
}

func TestFindFuzzyMatch_BlockAnchor(t *testing.T) {
	content := `package main

func foo() {
	x := computeSomething()
	y := processResult(x)
	return y
}

func bar() {
	return 42
}`

	// Search has slightly different interior but same anchors.
	search := `func foo() {
	x := computeOther()
	y := processResult(x)
	return y
}`

	m := FindFuzzyMatch(content, search)
	if m == nil {
		t.Fatal("expected block-anchor match")
	}
	if m.Level != 3 {
		t.Errorf("expected level 3 (block-anchor), got %d", m.Level)
	}
}

func TestFindFuzzyMatch_IndentNormalized(t *testing.T) {
	// Content has code at 8-space indent.
	content := `func outer() {
        if condition {
            x := compute()
            y := process(x)
            return y
        }
}`
	// Search has same code at 4-space indent (LLM produced wrong indent level).
	search := `    if condition {
        x := compute()
        y := process(x)
        return y
    }`

	m := FindFuzzyMatch(content, search)
	if m == nil {
		t.Fatal("expected indentation-normalized match")
	}
	if m.Level != 2 {
		t.Errorf("expected level 2 (indent-normalized), got %d", m.Level)
	}

	// Verify the matched region contains the right content.
	matched := content[m.StartByte:m.EndByte]
	if !containsFuzzy(matched, "x := compute()") {
		t.Errorf("matched region should contain 'x := compute()', got: %q", matched)
	}
}

func TestFindFuzzyMatch_IndentNormalized_Tabs(t *testing.T) {
	// Content uses double-tab indent.
	content := "func main() {\n\t\tif x > 0 {\n\t\t\treturn x\n\t\t}\n}"
	// Search uses single-tab indent.
	search := "\tif x > 0 {\n\t\treturn x\n\t}"

	m := FindFuzzyMatch(content, search)
	if m == nil {
		t.Fatal("expected indentation-normalized match for tabs")
	}
	if m.Level != 2 {
		t.Errorf("expected level 2 (indent-normalized), got %d", m.Level)
	}
}

func TestFindFuzzyMatch_NoMatch(t *testing.T) {
	content := "func main() {\n\treturn 0\n}"
	search := "completely different content that does not exist anywhere"

	m := FindFuzzyMatch(content, search)
	if m != nil {
		t.Errorf("expected no match, got level %d", m.Level)
	}
}

func TestEditFile_FuzzyFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")

	// Write a file with trailing whitespace.
	content := "func main() {  \n\tfmt.Println(\"hello\")  \n}  \n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Try to edit with a search string that lacks trailing whitespace.
	err := EditFile(EditFileParams{
		Path:      path,
		OldString: "func main() {\n\tfmt.Println(\"hello\")\n}",
		NewString: "func main() {\n\tfmt.Println(\"world\")\n}",
	})
	if err != nil {
		t.Fatalf("expected fuzzy match to succeed: %v", err)
	}

	// Verify the replacement was made.
	data, _ := os.ReadFile(path)
	result := string(data)
	if !containsFuzzy(result, "world") {
		t.Errorf("expected 'world' in result, got: %s", result)
	}
}

func TestEditFile_FuzzyFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")

	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := EditFile(EditFileParams{
		Path:      path,
		OldString: "completely nonexistent text",
		NewString: "replacement",
	})
	if err == nil {
		t.Fatal("expected error for non-matching edit")
	}
	// Should contain recovery hint.
	if !containsFuzzy(err.Error(), "grep_search") {
		t.Errorf("expected recovery hint in error: %s", err.Error())
	}
}

func TestTrimTrailingPerLine(t *testing.T) {
	input := "hello   \nworld  \n  indented  \n"
	want := "hello\nworld\n  indented\n"
	got := trimTrailingPerLine(input)
	if got != want {
		t.Errorf("trimTrailingPerLine:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComputeInteriorSimilarity(t *testing.T) {
	a := []string{"func foo() {", "  return 42", "}"}
	b := []string{"func foo() {", "  return 42", "}"}

	sim := computeInteriorSimilarity(a, b)
	if sim < 0.99 {
		t.Errorf("identical blocks should have sim ~1.0, got %f", sim)
	}

	c := []string{"func foo() {", "  return 99", "}"}
	sim2 := computeInteriorSimilarity(a, c)
	if sim2 < 0.7 {
		t.Errorf("similar blocks should have sim >= 0.7, got %f", sim2)
	}
}

func TestLineOffsets(t *testing.T) {
	content := "line0\nline1\nline2\nline3"

	if lineStartOffset(content, 0) != 0 {
		t.Error("line 0 should start at 0")
	}
	if lineStartOffset(content, 1) != 6 {
		t.Errorf("line 1 should start at 6, got %d", lineStartOffset(content, 1))
	}
	if lineStartOffset(content, 2) != 12 {
		t.Errorf("line 2 should start at 12, got %d", lineStartOffset(content, 2))
	}
}

func containsFuzzy(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
