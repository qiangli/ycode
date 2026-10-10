package api

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

// systemBlocksOnWire pulls the "system" field out of a captured Anthropic
// request body as the array form.
func systemBlocksOnWire(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	body := decodeBody(t, data)
	raw, err := json.Marshal(body["system"])
	if err != nil {
		t.Fatal(err)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("system is not an array of blocks: %v (%s)", err, data)
	}
	return blocks
}

// A YAML-declared fragment cache break (cache.breakAfter) arrives as its own
// system block; Anthropic's prompt cache can only reuse a prefix that ends at
// a cache_control marker, so the break must become one.
func TestAnthropicMarksCacheControlAtDeclaredBreak(t *testing.T) {
	var sink captureBody
	server := captureServer(t, &sink)
	client := NewAnthropicClient("k", WithBaseURL(server.URL+"/v1/messages"))
	request := &Request{
		Model:     "claude-test",
		MaxTokens: 256,
		SystemBlocks: []SystemBlock{
			{Type: "text", Text: "stable identity", CacheBreak: true},
			{Type: "text", Text: "workspace instructions", CacheBreak: true},
			{Type: "text", Text: "this turn's mode instruction"},
		},
		Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
	}
	drainStream(client.Send(context.Background(), request))

	blocks := systemBlocksOnWire(t, sink.body)
	if len(blocks) != 3 {
		t.Fatalf("system blocks = %d, want 3: %s", len(blocks), sink.body)
	}
	if blocks[0]["text"] != "stable identity" {
		t.Fatalf("stable prefix must come first, got %v", blocks[0]["text"])
	}
	marked := []int{}
	for i, block := range blocks {
		if block["cache_control"] != nil {
			marked = append(marked, i)
		}
	}
	// The declared break after the workspace fragment and the end of the
	// system prompt are the two breakpoints worth spending; the per-turn
	// block itself is never a cache prefix.
	want := []int{1, 2}
	if len(marked) != len(want) {
		t.Fatalf("cache_control at %v, want %v: %s", marked, want, sink.body)
	}
	for i := range want {
		if marked[i] != want[i] {
			t.Fatalf("cache_control at %v, want %v: %s", marked, want, sink.body)
		}
	}
}

// A plain System string keeps the single-block behavior: one cache mark at the
// end of the system prompt.
func TestAnthropicKeepsSingleCacheMarkForPlainSystem(t *testing.T) {
	var sink captureBody
	server := captureServer(t, &sink)
	client := NewAnthropicClient("k", WithBaseURL(server.URL+"/v1/messages"))
	request := &Request{
		Model:     "claude-test",
		MaxTokens: 256,
		System:    "be brief",
		Messages:  []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
	}
	drainStream(client.Send(context.Background(), request))

	blocks := systemBlocksOnWire(t, sink.body)
	if len(blocks) != 1 || blocks[0]["cache_control"] == nil {
		t.Fatalf("plain system must be one cached block: %s", sink.body)
	}
}

// CacheBreak is harness intent, not an Anthropic field: it must never reach
// the wire as a block key.
func TestSystemBlockCacheBreakStaysOffTheWire(t *testing.T) {
	data, err := json.Marshal(Request{
		Model:        "claude-test",
		MaxTokens:    1,
		SystemBlocks: []SystemBlock{{Type: "text", Text: "a", CacheBreak: true}},
		Messages:     []Message{{Role: RoleUser}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"CacheBreak"`, `"cache_break"`} {
		if bytes.Contains(data, []byte(key)) {
			t.Fatalf("%s leaked onto the wire: %s", key, data)
		}
	}
}

// A compat provider that is handed only the segmented system prompt must still
// send one, in declared order: the stable prefix first.
func TestOpenAICompatSystemFromBlocksIsPrefixStable(t *testing.T) {
	client := NewOpenAICompatClient("k", "http://x/v1")
	wire := client.buildRequest(&Request{
		Model:     "glm-5.3",
		MaxTokens: 256,
		SystemBlocks: []SystemBlock{
			{Type: "text", Text: "stable identity", CacheBreak: true},
			{Type: "text", Text: "this turn's mode instruction"},
		},
		Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
	})
	if len(wire.Messages) == 0 || wire.Messages[0].Role != "system" {
		t.Fatalf("first wire message must be the system prompt: %#v", wire.Messages)
	}
	if wire.Messages[0].Content != "stable identity\n\nthis turn's mode instruction" {
		t.Fatalf("system content = %q", wire.Messages[0].Content)
	}
}
