package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	netutil "github.com/qiangli/ycode/internal/runtime/net"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
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
		URL          string `json:"url"`
		Prompt       string `json:"prompt,omitempty"`
		OutputFormat string `json:"output_format,omitempty"`
		MaxLength    int    `json:"max_length,omitempty"`
		ClickLink    int    `json:"click_link,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse WebFetch input: %w", err)
	}

	// Text browser fallback: click_link resolves a link from the previous fetch.
	if params.ClickLink > 0 {
		link, ok := lookupLink(params.ClickLink)
		if !ok {
			return "", fmt.Errorf("link [%d] not found (use WebFetch with a URL first)", params.ClickLink)
		}
		params.URL = link.Href
	}

	if params.URL == "" {
		return "", fmt.Errorf("url is required (or use click_link to follow a link from the previous page)")
	}

	if err := netutil.ValidateURL(params.URL); err != nil {
		return "", fmt.Errorf("SSRF protection: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if err := netutil.ValidateURL(req.URL.String()); err != nil {
				return fmt.Errorf("SSRF protection on redirect: %w", err)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, params.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "ycode/1.0")

	host := req.URL.Host
	if host == "" {
		if u, perr := url.Parse(params.URL); perr == nil {
			host = u.Host
		}
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		yotel.RecordWebFetch(ctx, host, 0, time.Since(started), 0, false)
		return "", fmt.Errorf("fetch %s: %w", params.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		yotel.RecordWebFetch(ctx, host, resp.StatusCode, time.Since(started), 0, false)
		return "", fmt.Errorf("read response: %w", err)
	}
	yotel.RecordWebFetch(ctx, host, resp.StatusCode, time.Since(started), len(body), resp.StatusCode/100 == 2)

	text, err := extractContent(string(body), params.URL, params.OutputFormat, params.MaxLength)
	if err != nil {
		return "", fmt.Errorf("extract content: %w", err)
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

	started := time.Now()
	resp, err := searchWithFallback(ctx, params.Query, params.MaxResults)
	if err != nil {
		yotel.RecordWebSearch(ctx, "fallback", time.Since(started), 0, false)
		return "", fmt.Errorf("search: %w", err)
	}
	yotel.RecordWebSearch(ctx, "fallback", time.Since(started), len(resp.Results), true)

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	return string(out), nil
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
