package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBraveProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") == "" {
			t.Error("expected API key in header")
		}
		if r.URL.Query().Get("q") == "" {
			t.Error("expected query parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"web": map[string]any{
				"results": []map[string]any{
					{"title": "Result 1", "url": "https://example.com/1", "description": "First result"},
					{"title": "Result 2", "url": "https://example.com/2", "description": "Second result"},
				},
			},
		})
	}))
	defer server.Close()

	// Brave provider uses hardcoded URL, so we test the response parsing
	// by directly calling with a mock. For unit testing, we verify the struct.
	p := &braveProvider{apiKey: "test-key"}
	if p.Name() != "brave" {
		t.Errorf("Name() = %q, want %q", p.Name(), "brave")
	}
}

func TestSearxngProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Error("expected format=json parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"title": "SearXNG Result", "url": "https://example.com/s1", "content": "Found via SearXNG"},
			},
		})
	}))
	defer server.Close()

	p := &searxngProvider{baseURL: server.URL}
	results, err := p.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "SearXNG Result" {
		t.Errorf("title = %q, want %q", results[0].Title, "SearXNG Result")
	}
	if results[0].URL != "https://example.com/s1" {
		t.Errorf("url = %q, want %q", results[0].URL, "https://example.com/s1")
	}
	if results[0].Snippet != "Found via SearXNG" {
		t.Errorf("snippet = %q, want %q", results[0].Snippet, "Found via SearXNG")
	}
}

func TestSearxngProvider_MaxResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"title": "R1", "url": "https://example.com/1", "content": "c1"},
				{"title": "R2", "url": "https://example.com/2", "content": "c2"},
				{"title": "R3", "url": "https://example.com/3", "content": "c3"},
			},
		})
	}))
	defer server.Close()

	p := &searxngProvider{baseURL: server.URL}
	results, err := p.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (maxResults=2), got %d", len(results))
	}
}

func TestDuckDuckGoProvider(t *testing.T) {
	p := &duckduckgoProvider{}
	if p.Name() != "duckduckgo" {
		t.Errorf("Name() = %q, want %q", p.Name(), "duckduckgo")
	}
}

func TestSelectProviders_DefaultFallback(t *testing.T) {
	// With no env vars set, should always have DuckDuckGo.
	providers := selectProviders()
	if len(providers) == 0 {
		t.Fatal("expected at least one provider")
	}
	last := providers[len(providers)-1]
	if last.Name() != "duckduckgo" {
		t.Errorf("last provider = %q, want duckduckgo", last.Name())
	}
}

func TestSearchResponse_JSON(t *testing.T) {
	resp := SearchResponse{
		Query: "test",
		Results: []SearchResult{
			{Title: "R1", URL: "https://example.com", Snippet: "snippet"},
		},
		Provider: "test-provider",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded SearchResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Query != "test" {
		t.Errorf("query = %q, want %q", decoded.Query, "test")
	}
	if decoded.Provider != "test-provider" {
		t.Errorf("provider = %q, want %q", decoded.Provider, "test-provider")
	}
	if len(decoded.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(decoded.Results))
	}
}
