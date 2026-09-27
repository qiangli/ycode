package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		ok           bool
	}{
		{"POST", "/sticky/k1/v1/chat/completions", true},
		{"POST", "/sticky/k1/anthropic/v1/messages", true},
		{"GET", "/sticky/k1/v1/models", true},
		{"POST", "/sticky/k2/v1/chat/completions", false},
		{"POST", "/v1/chat/completions", false},
		{"POST", "/sticky/k1/v1/sticky", false},
		{"POST", "/sticky/k1/k/tok/v1/chat/completions", false},
		{"POST", "/sticky/k1/api/pull", false},
		{"GET", "/sticky/k1/v1/chat/completions", false},
	} {
		if _, ok := allowed(tc.method, tc.path, "k1"); ok != tc.ok {
			t.Errorf("allowed(%s %s) = %v, want %v", tc.method, tc.path, ok, tc.ok)
		}
	}
}

func TestProxyForwardsOnlyTheBinding(t *testing.T) {
	var got, auth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL + "/k/TOK")
	p := httptest.NewServer(proxy(u, "k1"))
	defer p.Close()
	req, _ := http.NewRequest("POST", p.URL+"/sticky/k1/v1/chat/completions", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer contained")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || got != "/k/TOK/sticky/k1/v1/chat/completions" || auth != "" {
		t.Errorf("status %d, upstream path %q auth %q", resp.StatusCode, got, auth)
	}
	got = ""
	resp, _ = http.Post(p.URL+"/v1/sticky", "application/json", strings.NewReader("{}"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || got != "" {
		t.Errorf("binding API: %d, upstream %q", resp.StatusCode, got)
	}
}
