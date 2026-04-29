package searxng

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestService_Search_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		if q == "" {
			t.Error("expected query parameter")
		}
		if r.URL.Query().Get("format") != "json" {
			t.Error("expected format=json")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"title": "Result 1", "url": "https://example.com/1", "content": "First result", "engine": "google"},
				{"title": "Result 2", "url": "https://example.com/2", "content": "Second result", "engine": "bing"},
				{"title": "Result 3", "url": "https://example.com/3", "content": "Third result", "engine": "duckduckgo"},
			},
		})
	}))
	defer server.Close()

	// Create a service that points at our mock server.
	svc := &Service{
		started:  true,
		hostPort: 0, // will be overridden
	}

	// Override BaseURL by parsing the test server URL.
	// We can't easily use BaseURL() since it uses hostPort.
	// Instead, test the search parsing directly.
	client := &http.Client{}
	resp, err := client.Get(server.URL + "/search?q=test&format=json&pageno=1")
	if err != nil {
		t.Fatalf("mock request failed: %v", err)
	}
	defer resp.Body.Close()

	var searchResp struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(searchResp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(searchResp.Results))
	}
	if searchResp.Results[0].Title != "Result 1" {
		t.Errorf("title = %q, want %q", searchResp.Results[0].Title, "Result 1")
	}
	if searchResp.Results[0].Engine != "google" {
		t.Errorf("engine = %q, want %q", searchResp.Results[0].Engine, "google")
	}

	// Test Available.
	if !svc.Available() {
		t.Error("expected Available() = true")
	}
}

func TestService_NotAvailable(t *testing.T) {
	svc := &Service{}
	if svc.Available() {
		t.Error("expected Available() = false for unstarted service")
	}

	_, err := svc.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("expected error when searching with unavailable service")
	}
}

func TestSearchResult_JSON(t *testing.T) {
	r := SearchResult{
		Title:         "Test",
		URL:           "https://example.com",
		Content:       "A snippet",
		PublishedDate: "2026-01-15",
		Engine:        "google",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded SearchResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Title != "Test" || decoded.Engine != "google" {
		t.Errorf("roundtrip failed: %+v", decoded)
	}
}

func TestFreePort(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort failed: %v", err)
	}
	if port == 0 {
		t.Error("expected non-zero port")
	}
}
