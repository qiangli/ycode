package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Sprint 412 story #1819 audit (step 1): some OpenAI-compatible gateways
// still emit the legacy (pre-tool_calls) function_call shape. ycode's chat
// path ignored it while mapping finish_reason "function_call" to tool_use,
// so the turn carried a tool_use outcome with zero calls. Both the
// streaming and the non-streaming readers must surface it as a tool_use
// block like any other call.

func collectCompatEvents(t *testing.T, events <-chan *StreamEvent) (tool *ContentBlock, stopReason string) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			switch ev.Type {
			case "content_block_start":
				block := *ev.ContentBlock
				tool = &block
			case "message_delta":
				var delta struct {
					StopReason string `json:"stop_reason"`
				}
				if err := json.Unmarshal(ev.Delta, &delta); err != nil {
					t.Fatalf("decode delta: %v", err)
				}
				if delta.StopReason != "" {
					stopReason = delta.StopReason
				}
			case "message_stop":
				return tool, stopReason
			}
		case <-timeout:
			t.Fatal("timed out waiting for message_stop")
		}
	}
}

func TestOpenAICompatStreamLegacyFunctionCall(t *testing.T) {
	var b strings.Builder
	b.WriteString("data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"bashy\",\"arguments\":\"{\\\"script\\\":\\\"\"}}}]}\n\n")
	b.WriteString("data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\"printf hi\\\"}\"}}}]}\n\n")
	b.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"function_call\"}]}\n\n")
	b.WriteString("data: [DONE]\n\n")

	c := NewOpenAICompatClient("k", "http://example.invalid")
	events := make(chan *StreamEvent, 16)
	errc := make(chan error, 1)
	go c.readStream(strings.NewReader(b.String()), events, errc)

	tool, stopReason := collectCompatEvents(t, events)
	if tool == nil {
		t.Fatal("no tool_use block for the legacy function_call deltas")
	}
	if tool.Name != "bashy" {
		t.Fatalf("tool name = %q, want bashy", tool.Name)
	}
	if string(tool.Input) != `{"script":"printf hi"}` {
		t.Fatalf("tool input = %s, want the reassembled arguments", tool.Input)
	}
	if stopReason != StopReasonToolUse {
		t.Fatalf("stop reason = %q, want %q", stopReason, StopReasonToolUse)
	}
}

func TestOpenAICompatNonStreamLegacyFunctionCall(t *testing.T) {
	body := `{"choices":[{"message":{"role":"assistant","content":null,"function_call":{"name":"bashy","arguments":"{\"script\":\"printf hi\"}"}},"finish_reason":"function_call"}]}`
	c := NewOpenAICompatClient("k", "http://example.invalid")
	events := make(chan *StreamEvent, 16)
	errc := make(chan error, 1)
	go c.readNonStream(strings.NewReader(body), events, errc)

	tool, stopReason := collectCompatEvents(t, events)
	if tool == nil {
		t.Fatal("no tool_use block for the legacy function_call message")
	}
	if tool.Name != "bashy" {
		t.Fatalf("tool name = %q, want bashy", tool.Name)
	}
	if string(tool.Input) != `{"script":"printf hi"}` {
		t.Fatalf("tool input = %s, want the function arguments", tool.Input)
	}
	if stopReason != StopReasonToolUse {
		t.Fatalf("stop reason = %q, want %q", stopReason, StopReasonToolUse)
	}
}
