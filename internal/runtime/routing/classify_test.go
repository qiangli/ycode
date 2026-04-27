package routing

import (
	"testing"
)

func TestParseCategories_ValidJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`["git","observability"]`, []string{"git", "observability"}},
		{`["memory"]`, []string{"memory"}},
		{`[]`, nil},
		{`["git"]`, []string{"git"}},
	}

	for _, tt := range tests {
		result := parseCategories(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseCategories(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseCategories(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestParseCategories_MarkdownFences(t *testing.T) {
	result := parseCategories("```json\n[\"git\"]\n```")
	if len(result) != 1 || result[0] != "git" {
		t.Errorf("should parse markdown-fenced JSON, got %v", result)
	}
}

func TestParseCategories_InvalidJSON(t *testing.T) {
	result := parseCategories("not json at all")
	if result != nil {
		t.Errorf("invalid JSON should return nil, got %v", result)
	}
}

func TestParseCategories_UnknownCategoriesFiltered(t *testing.T) {
	result := parseCategories(`["git","unknown_category","observability"]`)
	if len(result) != 2 {
		t.Fatalf("expected 2 valid categories, got %d: %v", len(result), result)
	}
	if result[0] != "git" || result[1] != "observability" {
		t.Errorf("expected [git, observability], got %v", result)
	}
}

func TestParseCategories_WhitespaceHandling(t *testing.T) {
	result := parseCategories(`  ["git"]  `)
	if len(result) != 1 {
		t.Errorf("should handle whitespace, got %v", result)
	}
}

func TestCategoryToolBundles_AllCategoriesHaveTools(t *testing.T) {
	for cat, tools := range categoryToolBundles {
		if len(tools) == 0 {
			t.Errorf("category %q has no tools", cat)
		}
	}
}

func TestCategoryToolBundles_KnownCategories(t *testing.T) {
	expected := []string{"git", "observability", "memory", "file_ops", "web", "agent"}
	for _, cat := range expected {
		if _, ok := categoryToolBundles[cat]; !ok {
			t.Errorf("missing expected category %q", cat)
		}
	}
}
