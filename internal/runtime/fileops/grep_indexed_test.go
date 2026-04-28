package fileops

import (
	"testing"
)

func TestExtractLiterals(t *testing.T) {
	tests := []struct {
		pattern string
		want    []string
	}{
		// Simple literal.
		{"handleLogin", []string{"handleLogin"}},
		// Literal with regex wildcards.
		{`func\s+Handle.*Request`, []string{"func", "Handle", "Request"}},
		// Case-insensitive flag folds to uppercase in regex AST.
		{`(?i)searchIndex`, []string{"SEARCHINDEX"}},
		// Short literals filtered out.
		{`a.b`, nil}, // "a" and "b" are < 3 chars
		// Alternation — no extraction (can't guarantee which branch).
		{`foo|bar`, nil},
		// Pure wildcard.
		{`.*`, nil},
		// Mixed literal and wildcard.
		{`func\s+(\w+)\s+error`, []string{"func", "error"}},
		// Escaped dot is a literal — concatenates to full string.
		{`fmt\.Println`, []string{"fmt.Println"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := extractLiterals(tt.pattern)
			if len(got) != len(tt.want) {
				t.Fatalf("extractLiterals(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
			for i, g := range got {
				if g != tt.want[i] {
					t.Errorf("extractLiterals(%q)[%d] = %q, want %q", tt.pattern, i, g, tt.want[i])
				}
			}
		})
	}
}

func TestIndexedGrepSearch_NilIndex(t *testing.T) {
	// With nil index, should fall back to regular GrepSearch.
	dir := t.TempDir()
	result, err := IndexedGrepSearch(GrepParams{
		Pattern: "nonexistent",
		Path:    dir,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
