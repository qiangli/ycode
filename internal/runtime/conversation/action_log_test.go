package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/observe"
	"github.com/qiangli/ycode/internal/tools"
)

// scriptedProvider replays a fixed sequence of turns: each call to Send returns
// the next scripted stream. This drives the real Runtime.Turn path end-to-end
// without a live LLM, so the action-log wiring is testable in CI.
type scriptedProvider struct {
	mu    sync.Mutex
	kind  api.ProviderKind
	turns []func() (<-chan *api.StreamEvent, <-chan error)
	i     int
}

func (p *scriptedProvider) Kind() api.ProviderKind { return p.kind }

func (p *scriptedProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn := p.turns[p.i]
	if p.i < len(p.turns)-1 {
		p.i++
	}
	return fn()
}

// toolUseTurn streams one tool_use block plus usage and stop_reason=tool_use.
func toolUseTurn(id, name, input string, inTok, outTok int) func() (<-chan *api.StreamEvent, <-chan error) {
	return func() (<-chan *api.StreamEvent, <-chan error) {
		events := make(chan *api.StreamEvent, 5)
		events <- &api.StreamEvent{Type: "message_start", Message: &api.Response{Usage: api.Usage{InputTokens: inTok}}}
		events <- &api.StreamEvent{Type: "content_block_start", ContentBlock: &api.ContentBlock{
			Type: api.ContentTypeToolUse, ID: id, Name: name, Input: json.RawMessage(input),
		}}
		events <- &api.StreamEvent{Type: "content_block_stop"}
		events <- &api.StreamEvent{Type: "message_delta", Usage: &api.Usage{OutputTokens: outTok},
			Delta: json.RawMessage(`{"stop_reason":"tool_use"}`)}
		close(events)
		errCh := make(chan error)
		close(errCh)
		return events, errCh
	}
}

// textTurn streams a final text answer with end_turn.
func textTurn(text string, inTok, outTok int) func() (<-chan *api.StreamEvent, <-chan error) {
	return func() (<-chan *api.StreamEvent, <-chan error) {
		events := make(chan *api.StreamEvent, 5)
		events <- &api.StreamEvent{Type: "message_start", Message: &api.Response{Usage: api.Usage{InputTokens: inTok}}}
		events <- &api.StreamEvent{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeText}}
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: json.RawMessage(`{"type":"text","text":` + jsonString(text) + `}`)}
		events <- &api.StreamEvent{Type: "content_block_stop"}
		events <- &api.StreamEvent{Type: "message_delta", Usage: &api.Usage{OutputTokens: outTok},
			Delta: json.RawMessage(`{"stop_reason":"end_turn"}`)}
		close(events)
		errCh := make(chan error)
		close(errCh)
		return events, errCh
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestActionLog_EndToEnd drives a two-turn agentic loop through the real runtime
// (Turn → ExecuteTools → Turn) and asserts the JSONL action log captures served
// model per turn, the tool call with name+args+result+duration, and a summary
// that reconciles — without a live LLM provider.
func TestActionLog_EndToEnd(t *testing.T) {
	provider := &scriptedProvider{
		kind: api.ProviderAnthropic,
		turns: []func() (<-chan *api.StreamEvent, <-chan error){
			toolUseTurn("call_1", "echo", `{"msg":"hi"}`, 1000, 50),
			textTurn("all done", 1200, 80),
		},
	}

	rt := newTestConversationRuntime(provider,
		&tools.ToolSpec{
			Name: "echo", Description: "echo", AlwaysAvailable: true,
			Handler: func(_ context.Context, input json.RawMessage) (string, error) {
				return "echoed: " + string(input), nil
			},
		},
	)

	buf := &bytes.Buffer{}
	rec := observe.New(observe.Options{Writer: buf, SessionID: "sess-e2e"})
	rt.SetActionRecorder(rec)

	ctx := context.Background()
	messages := []api.Message{{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "run echo"}}}}

	// Turn 1: model requests a tool.
	res1, err := rt.Turn(ctx, messages)
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if len(res1.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res1.ToolCalls))
	}
	// Execute the tools (records the tool call + flushes turn 1).
	toolResults := rt.ExecuteTools(ctx, res1.ToolCalls, nil)

	// Turn 2: append the assistant tool_use + tool results (as the agentic loop
	// does) so the request differs and does not hit the completion cache.
	messages2 := append([]api.Message{}, messages...)
	messages2 = append(messages2,
		api.Message{Role: api.RoleAssistant, Content: []api.ContentBlock{{
			Type: api.ContentTypeToolUse, ID: "call_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`),
		}}},
		api.Message{Role: api.RoleUser, Content: toolResults},
	)
	if _, err := rt.Turn(ctx, messages2); err != nil {
		t.Fatalf("turn 2: %v", err)
	}

	summary := rec.Finish()

	// Parse the JSONL.
	var turns []observe.TurnRecord
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("bad JSONL line: %v", err)
		}
		if probe.Type == observe.KindTurn {
			var tr observe.TurnRecord
			if err := json.Unmarshal([]byte(line), &tr); err != nil {
				t.Fatalf("bad turn: %v", err)
			}
			turns = append(turns, tr)
		}
	}

	if len(turns) != 2 {
		t.Fatalf("expected 2 turn records, got %d", len(turns))
	}

	// GATE: served_model per turn.
	for i, tr := range turns {
		if tr.Request.ServedModel != "test-model" {
			t.Errorf("turn %d served_model = %q, want test-model", i, tr.Request.ServedModel)
		}
		if tr.Request.PromptHash == "" {
			t.Errorf("turn %d missing prompt hash", i)
		}
	}

	// GATE: the tool call appears with name + args + result + duration on turn 1.
	tc := turns[0].ToolCalls
	if len(tc) != 1 {
		t.Fatalf("turn 1 tool calls = %d, want 1", len(tc))
	}
	if tc[0].Name != "echo" {
		t.Errorf("tool name = %q, want echo", tc[0].Name)
	}
	if !strings.Contains(tc[0].Arguments, "hi") {
		t.Errorf("tool arguments not captured: %q", tc[0].Arguments)
	}
	if !strings.Contains(tc[0].Result, "echoed") {
		t.Errorf("tool result not captured: %q", tc[0].Result)
	}
	if tc[0].Status != observe.StatusOK {
		t.Errorf("tool status = %q, want ok", tc[0].Status)
	}

	// Response usage captured.
	if turns[0].Response.PromptTokens != 1000 || turns[0].Response.CompletionTokens != 50 {
		t.Errorf("turn 1 usage = %d/%d, want 1000/50", turns[0].Response.PromptTokens, turns[0].Response.CompletionTokens)
	}
	if turns[0].Response.FinishReason != "tool_use" {
		t.Errorf("turn 1 finish_reason = %q, want tool_use", turns[0].Response.FinishReason)
	}

	// GATE: summary reconciles with the per-turn records.
	if summary.Turns != 2 {
		t.Errorf("summary turns = %d, want 2", summary.Turns)
	}
	if summary.PromptTokens != 2200 || summary.CompletionTokens != 130 {
		t.Errorf("summary tokens = %d/%d, want 2200/130", summary.PromptTokens, summary.CompletionTokens)
	}
	if summary.ToolCalls != 1 || summary.PerTool["echo"] == nil || summary.PerTool["echo"].Calls != 1 {
		t.Errorf("summary tool totals wrong: %+v", summary.PerTool)
	}
}
