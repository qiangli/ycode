package tools

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseHTML(s string) (*html.Node, error) {
	return html.Parse(strings.NewReader(s))
}

func TestExtractContent_Markdown(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head><title>Test Article</title>
<meta name="description" content="A test article for extraction.">
</head>
<body>
<nav><a href="/">Home</a> | <a href="/about">About</a></nav>
<article>
<h1>Test Article Title</h1>
<p>This is the first paragraph of a test article with enough content
to be considered the main article by the readability algorithm. It needs
to be reasonably long to pass readability heuristics.</p>
<p>Here is a second paragraph with more content. The readability algorithm
needs multiple paragraphs to determine that this is indeed the main
content of the page and not just a sidebar or navigation element.</p>
<p>A third paragraph adds more weight. Links like <a href="https://example.com">example</a>
should be preserved in the markdown output as proper markdown links.</p>
</article>
<footer>Copyright 2026</footer>
</body></html>`

	result, err := extractContent(html, "https://example.com/article", "markdown", 0)
	if err != nil {
		t.Fatalf("extractContent failed: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Should contain metadata header.
	if !containsStr(result, "Title:") {
		t.Error("expected Title: in result")
	}
	if !containsStr(result, "URL: https://example.com/article") {
		t.Error("expected URL in result")
	}
	// Should contain word count.
	if !containsStr(result, "Word count:") {
		t.Error("expected Word count in result")
	}
}

func TestExtractContent_Text(t *testing.T) {
	html := `<html><body>
<article><h1>Hello</h1><p>World paragraph one. This needs to be long enough
for readability to pick it up as main content of the page.</p>
<p>Second paragraph with more text to ensure readability considers this the article.</p></article>
</body></html>`

	result, err := extractContent(html, "https://example.com", "text", 0)
	if err != nil {
		t.Fatalf("extractContent text failed: %v", err)
	}

	if !containsStr(result, "World") {
		t.Error("expected 'World' in text output")
	}
}

func TestExtractContent_MetadataOnly(t *testing.T) {
	html := `<html><head><title>My Page</title></head>
<body><article><p>Content here that is long enough for readability to parse properly
and extract as the main article content of this test page.</p>
<p>More content in a second paragraph.</p></article></body></html>`

	result, err := extractContent(html, "https://example.com", "metadata_only", 0)
	if err != nil {
		t.Fatalf("extractContent metadata_only failed: %v", err)
	}

	if !containsStr(result, "URL: https://example.com") {
		t.Error("expected URL in metadata")
	}
	// Should NOT contain the article body.
	if containsStr(result, "Word count:") {
		t.Error("metadata_only should not contain word count")
	}
}

func TestExtractContent_Fallback(t *testing.T) {
	// Non-article HTML should still produce output via fallback.
	html := `<div>Just some text without article structure</div>`

	result, err := extractContent(html, "https://example.com", "markdown", 0)
	if err != nil {
		t.Fatalf("extractContent fallback failed: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty fallback result")
	}
}

func TestSmartTruncate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLen    int
		wantTrunc bool
	}{
		{"short", "hello", 100, false},
		{"exact", "hello", 5, false},
		{"truncate_paragraph", "line1\n\nline2\n\nline3\n\nline4", 20, true},
		{"truncate_line", "line1\nline2\nline3\nline4\nline5", 20, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := smartTruncate(tt.input, tt.maxLen)
			if tt.wantTrunc && !containsStr(result, "truncated") {
				t.Errorf("expected truncation marker, got: %s", result)
			}
			if !tt.wantTrunc && containsStr(result, "truncated") {
				t.Errorf("unexpected truncation for short input")
			}
		})
	}
}

func TestExtractContent_LinksExtraction(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<article>
<h1>Links Test</h1>
<p>This is a long enough paragraph for readability. It contains a link to
<a href="https://example.com/page1">Page One</a> and another to
<a href="https://example.com/page2">Page Two</a>. The content must be
sufficiently long for readability extraction to succeed.</p>
<p>Another paragraph with a <a href="/relative">relative link</a> that should
be resolved against the base URL. More text to make this article credible
to the readability algorithm.</p>
</article></body></html>`

	result, err := extractContent(html, "https://example.com/article", "markdown", 0)
	if err != nil {
		t.Fatalf("extractContent failed: %v", err)
	}

	if !containsStr(result, "Links:") {
		t.Error("expected Links: section in result")
	}
	if !containsStr(result, "[1]") {
		t.Error("expected numbered link [1]")
	}
	if !containsStr(result, "https://example.com/page1") {
		t.Error("expected page1 link in output")
	}
}

func TestExtractLinks_Dedup(t *testing.T) {
	// Duplicate href should appear only once.
	html := `<div>
<a href="https://example.com">Link A</a>
<a href="https://example.com">Link B</a>
<a href="https://other.com">Other</a>
</div>`

	doc, _ := parseHTML(html)
	links := extractLinks(doc, nil)
	if len(links) != 2 {
		t.Errorf("expected 2 unique links, got %d", len(links))
	}
}

func TestLookupLink(t *testing.T) {
	storeLinks([]pageLink{
		{ID: 1, Text: "First", Href: "https://example.com/1"},
		{ID: 2, Text: "Second", Href: "https://example.com/2"},
	})

	l, ok := lookupLink(1)
	if !ok || l.Href != "https://example.com/1" {
		t.Errorf("lookupLink(1) = %v, %v", l, ok)
	}

	l, ok = lookupLink(2)
	if !ok || l.Href != "https://example.com/2" {
		t.Errorf("lookupLink(2) = %v, %v", l, ok)
	}

	_, ok = lookupLink(0)
	if ok {
		t.Error("lookupLink(0) should return false")
	}

	_, ok = lookupLink(3)
	if ok {
		t.Error("lookupLink(3) should return false")
	}
}

func TestCountWords(t *testing.T) {
	if n := countWords("hello world foo"); n != 3 {
		t.Errorf("countWords = %d, want 3", n)
	}
	if n := countWords(""); n != 0 {
		t.Errorf("countWords empty = %d, want 0", n)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
