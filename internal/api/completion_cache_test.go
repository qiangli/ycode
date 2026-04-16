package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompletionCache_LookupMiss(t *testing.T) {
	cc := NewCompletionCache("", 30*time.Second)
	resp := cc.Lookup("nonexistent")
	if resp != nil {
		t.Error("expected nil for cache miss")
	}
	if cc.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", cc.Misses)
	}
}

func TestCompletionCache_StoreAndLookup(t *testing.T) {
	cc := NewCompletionCache("", 30*time.Second)
	resp := &Response{
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "hello"},
		},
	}

	cc.Store("hash123", resp)
	got := cc.Lookup("hash123")
	if got == nil {
		t.Fatal("expected cache hit")
	}
	if got.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", got.StopReason)
	}
	if cc.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", cc.Hits)
	}
}

func TestCompletionCache_TTLExpiry(t *testing.T) {
	cc := NewCompletionCache("", 50*time.Millisecond)
	resp := &Response{StopReason: "end_turn"}

	cc.Store("hash456", resp)
	time.Sleep(100 * time.Millisecond)

	got := cc.Lookup("hash456")
	if got != nil {
		t.Error("expected nil after TTL expiry")
	}
}

func TestCompletionCache_Clear(t *testing.T) {
	cc := NewCompletionCache("", 30*time.Second)
	cc.Store("a", &Response{StopReason: "a"})
	cc.Store("b", &Response{StopReason: "b"})

	cc.Clear()

	if cc.Lookup("a") != nil || cc.Lookup("b") != nil {
		t.Error("expected all entries cleared")
	}
}

func TestCompletionCache_DiskPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	cc := NewCompletionCache(dir, 30*time.Second)
	resp := &Response{
		StopReason: "end_turn",
		Content:    []ContentBlock{{Type: ContentTypeText, Text: "cached"}},
	}

	cc.Store("diskhash", resp)
	// Wait for async disk write.
	time.Sleep(50 * time.Millisecond)

	// Verify file was created.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read cache dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one cache file on disk")
	}

	// Create a new cache instance (cold start) pointing at same dir.
	cc2 := NewCompletionCache(dir, 30*time.Second)
	got := cc2.Lookup("diskhash")
	if got == nil {
		t.Fatal("expected disk cache hit from new instance")
	}
	if got.StopReason != "end_turn" {
		t.Errorf("expected 'end_turn', got %q", got.StopReason)
	}
}

func TestRequestHash_Deterministic(t *testing.T) {
	req := &Request{
		Model:  "claude-sonnet-4-20250514",
		System: "You are helpful.",
		Messages: []Message{
			{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		},
	}

	h1 := RequestHash(req)
	h2 := RequestHash(req)
	if h1 != h2 {
		t.Errorf("request hash should be deterministic: %s vs %s", h1, h2)
	}
}

func TestRequestHash_DifferentMessages(t *testing.T) {
	req1 := &Request{Model: "m", System: "s", Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
	}}
	req2 := &Request{Model: "m", System: "s", Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "goodbye"}}},
	}}

	if RequestHash(req1) == RequestHash(req2) {
		t.Error("different messages should produce different hashes")
	}
}
