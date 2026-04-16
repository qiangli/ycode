package tools

import (
	"strings"
	"testing"
)

func TestIsCodexPatch(t *testing.T) {
	if !IsCodexPatch("*** Begin Patch\n*** End Patch") {
		t.Error("should detect Codex patch")
	}
	if !IsCodexPatch("  *** Begin Patch\n") {
		t.Error("should detect with leading whitespace")
	}
	if IsCodexPatch("--- a/file.go\n+++ b/file.go") {
		t.Error("should not detect unified diff as Codex patch")
	}
}

func TestParseCodexPatch_AddFile(t *testing.T) {
	input := `*** Begin Patch
*** Add File: hello.txt
+Hello world
+Second line
*** End Patch
`
	patch, err := ParseCodexPatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(patch.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(patch.Hunks))
	}
	h := patch.Hunks[0]
	if h.Op != CodexHunkAdd {
		t.Errorf("expected Add op, got %d", h.Op)
	}
	if h.Path != "hello.txt" {
		t.Errorf("expected path 'hello.txt', got %q", h.Path)
	}
	if len(h.AddLines) != 2 {
		t.Errorf("expected 2 add lines, got %d", len(h.AddLines))
	}
	if h.AddLines[0] != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", h.AddLines[0])
	}
}

func TestParseCodexPatch_DeleteFile(t *testing.T) {
	input := `*** Begin Patch
*** Delete File: obsolete.txt
*** End Patch
`
	patch, err := ParseCodexPatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(patch.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(patch.Hunks))
	}
	if patch.Hunks[0].Op != CodexHunkDelete {
		t.Error("expected Delete op")
	}
}

func TestParseCodexPatch_UpdateFile(t *testing.T) {
	input := `*** Begin Patch
*** Update File: src/main.go
@@ func main()
 	fmt.Println("before")
-	fmt.Println("old")
+	fmt.Println("new")
 	fmt.Println("after")
*** End Patch
`
	patch, err := ParseCodexPatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(patch.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(patch.Hunks))
	}
	h := patch.Hunks[0]
	if h.Op != CodexHunkUpdate {
		t.Error("expected Update op")
	}
	if len(h.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(h.Changes))
	}
	c := h.Changes[0]
	if c.ContextHint != "func main()" {
		t.Errorf("expected hint 'func main()', got %q", c.ContextHint)
	}
	// Should have 4 lines: context, -, +, context
	if len(c.Lines) != 4 {
		t.Errorf("expected 4 change lines, got %d", len(c.Lines))
	}
}

func TestParseCodexPatch_MultipleHunks(t *testing.T) {
	input := `*** Begin Patch
*** Add File: new.txt
+content
*** Delete File: old.txt
*** Update File: src/app.go
@@ func init()
-old line
+new line
*** End Patch
`
	patch, err := ParseCodexPatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(patch.Hunks) != 3 {
		t.Fatalf("expected 3 hunks, got %d", len(patch.Hunks))
	}
	if patch.Hunks[0].Op != CodexHunkAdd {
		t.Error("first hunk should be Add")
	}
	if patch.Hunks[1].Op != CodexHunkDelete {
		t.Error("second hunk should be Delete")
	}
	if patch.Hunks[2].Op != CodexHunkUpdate {
		t.Error("third hunk should be Update")
	}
}

func TestApplyCodexChange(t *testing.T) {
	fileLines := []string{
		"package main",
		"",
		"func main() {",
		"\tfmt.Println(\"hello\")",
		"\tfmt.Println(\"old\")",
		"\tfmt.Println(\"end\")",
		"}",
	}

	change := CodexChange{
		ContextHint: "func main()",
		Lines: []CodexLine{
			{Op: ' ', Text: "\tfmt.Println(\"hello\")"},
			{Op: '-', Text: "\tfmt.Println(\"old\")"},
			{Op: '+', Text: "\tfmt.Println(\"new\")"},
			{Op: ' ', Text: "\tfmt.Println(\"end\")"},
		},
	}

	result, err := applyCodexChange(fileLines, change)
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "\"new\"") {
		t.Error("should contain new line")
	}
	if strings.Contains(joined, "\"old\"") {
		t.Error("should not contain old line")
	}
}

func TestFindContextMatch(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"func foo() {",
		"\tline1",
		"\tline2",
		"}",
		"",
		"func bar() {",
		"\tline1",
		"\tline2",
		"}",
	}

	// Without hint, should find first match.
	idx := findContextMatch(lines, []string{"\tline1", "\tline2"}, "")
	if idx != 3 {
		t.Errorf("expected index 3, got %d", idx)
	}

	// With hint "func bar", should find the one inside bar.
	idx = findContextMatch(lines, []string{"\tline1", "\tline2"}, "func bar()")
	if idx != 8 {
		t.Errorf("expected index 8 (inside bar), got %d", idx)
	}
}
