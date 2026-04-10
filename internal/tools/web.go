package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RegisterWebHandlers registers WebFetch and WebSearch handlers.
func RegisterWebHandlers(r *Registry) {
	if spec, ok := r.Get("WebFetch"); ok {
		spec.Handler = handleWebFetch
	}
	if spec, ok := r.Get("WebSearch"); ok {
		spec.Handler = handleWebSearch
	}
}

func handleWebFetch(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		URL    string `json:"url"`
		Prompt string `json:"prompt,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse WebFetch input: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, params.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "ycode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", params.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024)) // 100KB limit
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	text := stripHTML(string(body))

	// Truncate to reasonable size.
	if len(text) > 8192 {
		text = text[:8192] + "\n... (truncated)"
	}

	return text, nil
}

func handleWebSearch(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse WebSearch input: %w", err)
	}

	// Simple DuckDuckGo HTML search.
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", params.Query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "ycode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	text := stripHTML(string(body))
	if len(text) > 4096 {
		text = text[:4096] + "\n... (truncated)"
	}

	return text, nil
}

// stripHTML does a basic HTML to text conversion.
func stripHTML(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse whitespace.
	result := b.String()
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}
