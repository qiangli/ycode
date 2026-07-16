package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatStreamLongReasoningBeforeToolDoesNotBlock(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"r%d \"}}]}\n\n", i)
	}
	b.WriteString("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n")
	b.WriteString("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"roman.go\\\"}\"}}]}}]}\n\n")
	b.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
	b.WriteString("data: [DONE]\n\n")

	c := NewOpenAICompatClient("k", "http://example.invalid")
	events := make(chan *StreamEvent, 8)
	errc := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		c.readStream(strings.NewReader(b.String()), events, errc)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readStream blocked on long reasoning-only prefix before tool call")
	}
	close(events)

	var thinkingDeltas int
	var thinking string
	var tool *ContentBlock
	var stopReason string
	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			var delta struct {
				Type     string `json:"type"`
				Thinking string `json:"thinking"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err != nil {
				t.Fatalf("decode delta: %v", err)
			}
			if delta.Type == "thinking_delta" {
				thinkingDeltas++
				thinking += delta.Thinking
			}
		case "content_block_start":
			block := *ev.ContentBlock
			tool = &block
		case "message_delta":
			var delta struct {
				StopReason string `json:"stop_reason"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.StopReason != "" {
				stopReason = delta.StopReason
			}
		}
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("readStream error: %v", err)
		}
	default:
	}

	if thinkingDeltas != 1 {
		t.Fatalf("thinking deltas = %d, want 1 coalesced delta", thinkingDeltas)
	}
	if !strings.Contains(thinking, "r0 ") || !strings.Contains(thinking, "r199 ") {
		t.Fatalf("coalesced thinking lost content: %q", thinking)
	}
	if tool == nil {
		t.Fatal("missing tool call after reasoning")
	}
	if tool.ID != "call_1" || tool.Name != "write_file" {
		t.Fatalf("tool = id %q name %q, want call_1 write_file", tool.ID, tool.Name)
	}
	if string(tool.Input) != `{"path":"roman.go"}` {
		t.Fatalf("tool input = %s", tool.Input)
	}
	if stopReason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use", stopReason)
	}
}
