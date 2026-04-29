package tools

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

// pageLink represents a link extracted from a page.
type pageLink struct {
	ID   int
	Text string
	Href string
}

// extractionResult holds structured content extracted from a web page.
type extractionResult struct {
	Title       string
	URL         string
	Description string
	Content     string
	Links       []pageLink
	WordCount   int
}

// linkStore is a session-level cache of links from the last WebFetch call,
// enabling the text browser fallback (click_link by ID).
var linkStore struct {
	mu    sync.Mutex
	links []pageLink
}

// storeLinks saves links for later click_link lookups.
func storeLinks(links []pageLink) {
	linkStore.mu.Lock()
	defer linkStore.mu.Unlock()
	linkStore.links = links
}

// lookupLink retrieves a stored link by its 1-based ID.
func lookupLink(id int) (pageLink, bool) {
	linkStore.mu.Lock()
	defer linkStore.mu.Unlock()
	if id < 1 || id > len(linkStore.links) {
		return pageLink{}, false
	}
	return linkStore.links[id-1], true
}

// extractContent parses HTML using readability and converts to the requested format.
// It extracts the main article content, stripping navigation, ads, and chrome.
func extractContent(rawHTML string, pageURL string, format string, maxLength int) (string, error) {
	if format == "" {
		format = "markdown"
	}
	if maxLength <= 0 {
		maxLength = 32768
	}

	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		parsedURL = &url.URL{}
	}

	article, err := readability.FromReader(strings.NewReader(rawHTML), parsedURL)
	if err != nil {
		// Readability failed; fall back to basic conversion.
		return extractFallback(rawHTML, format, maxLength)
	}

	result := extractionResult{
		Title:       article.Title(),
		URL:         pageURL,
		Description: article.Excerpt(),
	}

	// Extract links from the article node.
	if article.Node != nil {
		result.Links = extractLinks(article.Node, parsedURL)
		storeLinks(result.Links)
	}

	switch format {
	case "metadata_only":
		return formatMetadata(result), nil
	case "html":
		var buf bytes.Buffer
		if err := article.RenderHTML(&buf); err != nil {
			return extractFallback(rawHTML, format, maxLength)
		}
		result.Content = buf.String()
	case "text":
		var buf bytes.Buffer
		if err := article.RenderText(&buf); err != nil {
			return extractFallback(rawHTML, format, maxLength)
		}
		result.Content = buf.String()
	default: // "markdown"
		var htmlBuf bytes.Buffer
		if err := article.RenderHTML(&htmlBuf); err != nil {
			return extractFallback(rawHTML, format, maxLength)
		}
		md, err := htmltomarkdown.ConvertString(htmlBuf.String())
		if err != nil {
			// Fall back to plain text if markdown conversion fails.
			var textBuf bytes.Buffer
			if err := article.RenderText(&textBuf); err != nil {
				return extractFallback(rawHTML, format, maxLength)
			}
			result.Content = textBuf.String()
		} else {
			result.Content = md
		}
	}

	result.WordCount = countWords(result.Content)
	output := formatResult(result)
	return smartTruncate(output, maxLength), nil
}

// extractLinks walks an HTML node tree and extracts unique <a> links.
func extractLinks(node *html.Node, baseURL *url.URL) []pageLink {
	var links []pageLink
	seen := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := getAttr(n, "href")
			if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
				goto children
			}

			// Resolve relative URLs.
			if baseURL != nil {
				if u, err := baseURL.Parse(href); err == nil {
					href = u.String()
				}
			}

			if !seen[href] {
				seen[href] = true
				text := strings.TrimSpace(innerText(n))
				if text == "" {
					text = href
				}
				links = append(links, pageLink{
					ID:   len(links) + 1,
					Text: text,
					Href: href,
				})
			}
		}
	children:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	return links
}

// getAttr returns the value of an attribute on an HTML node.
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// innerText extracts text content from an HTML node and its children.
func innerText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(innerText(c))
	}
	return b.String()
}

// extractFallback provides basic HTML-to-text conversion when readability fails.
func extractFallback(rawHTML string, format string, maxLength int) (string, error) {
	if format == "html" {
		return smartTruncate(rawHTML, maxLength), nil
	}

	md, err := htmltomarkdown.ConvertString(rawHTML)
	if err != nil {
		// Last resort: basic tag stripping.
		text := stripHTML(rawHTML)
		return smartTruncate(text, maxLength), nil
	}

	if format == "text" {
		return smartTruncate(stripHTML(rawHTML), maxLength), nil
	}

	return smartTruncate(md, maxLength), nil
}

// formatMetadata returns only the metadata header without content.
func formatMetadata(r extractionResult) string {
	var b strings.Builder
	if r.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", r.Title)
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", r.URL)
	}
	if r.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", r.Description)
	}
	return strings.TrimSpace(b.String())
}

// formatResult produces the structured output with metadata header and content.
func formatResult(r extractionResult) string {
	var b strings.Builder
	if r.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", r.Title)
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", r.URL)
	}
	if r.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", r.Description)
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString(r.Content)

	b.WriteString("\n\n---\n")
	if len(r.Links) > 0 {
		b.WriteString("Links:")
		for _, l := range r.Links {
			fmt.Fprintf(&b, " [%d] %s (%s)", l.ID, l.Text, l.Href)
			if l.ID < len(r.Links) {
				b.WriteString(",")
			}
		}
		b.WriteString("\n")
	}
	if r.WordCount > 0 {
		fmt.Fprintf(&b, "Word count: %d\n", r.WordCount)
	}
	return b.String()
}

// smartTruncate truncates text at the nearest paragraph or line boundary
// within maxLength, avoiding mid-word or mid-sentence breaks.
func smartTruncate(text string, maxLength int) string {
	if utf8.RuneCountInString(text) <= maxLength {
		return text
	}

	// Convert to runes for proper Unicode handling.
	runes := []rune(text)
	if len(runes) <= maxLength {
		return text
	}

	// Look for a paragraph break (\n\n) near the limit.
	candidate := string(runes[:maxLength])
	if idx := strings.LastIndex(candidate, "\n\n"); idx > maxLength*3/4 {
		return candidate[:idx] + "\n\n... (truncated)"
	}

	// Fall back to line break.
	if idx := strings.LastIndex(candidate, "\n"); idx > maxLength*3/4 {
		return candidate[:idx] + "\n... (truncated)"
	}

	// Fall back to space.
	if idx := strings.LastIndex(candidate, " "); idx > maxLength*3/4 {
		return candidate[:idx] + " ... (truncated)"
	}

	return candidate + "... (truncated)"
}

// countWords returns a rough word count for the content.
func countWords(s string) int {
	return len(strings.Fields(s))
}
