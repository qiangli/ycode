package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// SearchResult represents a single search result from any provider.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Date    string `json:"date,omitempty"`
}

// SearchResponse is the structured output from WebSearch.
type SearchResponse struct {
	Query    string         `json:"query"`
	Results  []SearchResult `json:"results"`
	Provider string         `json:"provider"`
}

// SearchProvider defines the interface for web search backends.
type SearchProvider interface {
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
	Name() string
}

// containerSearXNG holds a reference to the containerized SearXNG service.
// When set, it takes priority over all other providers.
var containerSearXNG SearchProvider

// SetSearXNGProvider sets the containerized SearXNG as the top-priority search provider.
func SetSearXNGProvider(provider SearchProvider) {
	containerSearXNG = provider
}

// selectProviders returns available search providers in priority order.
// Priority: containerized SearXNG > Brave > Tavily > SearXNG URL > DuckDuckGo.
func selectProviders() []SearchProvider {
	var providers []SearchProvider

	// Containerized SearXNG gets top priority when available.
	if containerSearXNG != nil {
		providers = append(providers, containerSearXNG)
	}

	if key := os.Getenv("BRAVE_SEARCH_API_KEY"); key != "" {
		providers = append(providers, &braveProvider{apiKey: key})
	}
	if key := os.Getenv("TAVILY_API_KEY"); key != "" {
		providers = append(providers, &tavilyProvider{apiKey: key})
	}
	if u := os.Getenv("SEARXNG_URL"); u != "" {
		providers = append(providers, &searxngProvider{baseURL: u})
	}

	// DuckDuckGo is always available as fallback.
	providers = append(providers, &duckduckgoProvider{})

	return providers
}

// searchWithFallback tries providers in order, falling back on errors.
func searchWithFallback(ctx context.Context, query string, maxResults int) (*SearchResponse, error) {
	providers := selectProviders()
	var lastErr error

	for _, p := range providers {
		results, err := p.Search(ctx, query, maxResults)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", p.Name(), err)
			continue
		}
		return &SearchResponse{
			Query:    query,
			Results:  results,
			Provider: p.Name(),
		}, nil
	}

	return nil, fmt.Errorf("all search providers failed: %w", lastErr)
}

// braveProvider implements SearchProvider using the Brave Search API.
type braveProvider struct {
	apiKey string
}

func (p *braveProvider) Name() string { return "brave" }

func (p *braveProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	client := &http.Client{Timeout: 15 * time.Second}
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", p.apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var braveResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []SearchResult
	for _, r := range braveResp.Web.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
			Date:    r.Age,
		})
	}
	return results, nil
}

// tavilyProvider implements SearchProvider using the Tavily API.
type tavilyProvider struct {
	apiKey string
}

func (p *tavilyProvider) Name() string { return "tavily" }

func (p *tavilyProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	client := &http.Client{Timeout: 15 * time.Second}

	reqBody, err := json.Marshal(map[string]any{
		"query":       query,
		"max_results": maxResults,
		"api_key":     p.apiKey,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.tavily.com/search", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tavilyResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tavilyResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []SearchResult
	for _, r := range tavilyResp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}
	return results, nil
}

// searxngProvider implements SearchProvider using a SearXNG instance.
type searxngProvider struct {
	baseURL string
}

func (p *searxngProvider) Name() string { return "searxng" }

func (p *searxngProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	client := &http.Client{Timeout: 15 * time.Second}
	u := fmt.Sprintf("%s/search?q=%s&format=json&pageno=1",
		strings.TrimRight(p.baseURL, "/"), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var searxResp struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"publishedDate"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searxResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []SearchResult
	for i, r := range searxResp.Results {
		if i >= maxResults {
			break
		}
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
			Date:    r.PublishedDate,
		})
	}
	return results, nil
}

// duckduckgoProvider implements SearchProvider using DuckDuckGo's HTML search.
type duckduckgoProvider struct{}

func (p *duckduckgoProvider) Name() string { return "duckduckgo" }

func (p *duckduckgoProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	u := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ycode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return nil, err
	}

	// DuckDuckGo HTML doesn't give structured results easily.
	// Return as a single "result" with stripped text.
	text := stripHTML(string(body))
	if len(text) > 4096 {
		text = text[:4096]
	}

	return []SearchResult{
		{
			Title:   fmt.Sprintf("DuckDuckGo results for: %s", query),
			URL:     u,
			Snippet: text,
		},
	}, nil
}
