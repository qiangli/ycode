package widget

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	mcppkg "github.com/qiangli/ycode/internal/runtime/mcp"
)

// collect runs h.HandleToolCall and captures the first EventStateUpdate
// published to the in-memory bus. Helper so each test stays focused.
func collect(t *testing.T, h *MCPHandler, tool string, input string) (bus.Event, string) {
	t.Helper()
	ch, unsub := h.bus.Subscribe(bus.EventStateUpdate)
	defer unsub()

	res, err := h.HandleToolCall(context.Background(), tool, json.RawMessage(input))
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}

	select {
	case ev := <-ch:
		return ev, res
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no EventStateUpdate published within timeout", tool)
		return bus.Event{}, res
	}
}

func newHandler() *MCPHandler {
	return NewMCPHandler(bus.NewMemoryBus())
}

func TestListTools_BothToolsExposed(t *testing.T) {
	tools := newHandler().ListTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"agent_render_a2ui", "agent_render_widget"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestRequiredMode_ReadOnly(t *testing.T) {
	// Both tools publish to the bus; they don't touch disk/shell/network.
	// Anything stricter than ReadOnly would break trivial widget rendering.
	h := newHandler()
	for _, name := range []string{"agent_render_a2ui", "agent_render_widget"} {
		if got := h.RequiredMode(name); got != mcppkg.ModeReadOnly {
			t.Errorf("%s: RequiredMode = %q, want ReadOnly", name, got)
		}
	}
}

func TestRenderA2UI_PublishesEventStateUpdate(t *testing.T) {
	h := newHandler()
	ev, res := collect(t, h, "agent_render_a2ui",
		`{"ops":[{"version":"v0.9","createSurface":{"surfaceId":"health","catalogId":"x"}}],"session_id":"sess-1"}`)

	if ev.SessionID != "sess-1" {
		t.Errorf("session_id arg not honored; got %q want sess-1", ev.SessionID)
	}
	var payload map[string]any
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["format"] != "a2ui" {
		t.Errorf("format discriminator = %v, want a2ui", payload["format"])
	}
	body, _ := payload["body"].(string)
	// body should be a JSON-string wrapping the ops in OperationsKey.
	// Marshaled by a2ui.Render() → starts with {"a2ui_operations":[...
	if body == "" {
		// json.RawMessage round-trips as nested object — try direct read.
		raw, _ := json.Marshal(payload["body"])
		body = string(raw)
	}
	if !strings.Contains(body, "a2ui_operations") {
		t.Errorf("body missing a2ui_operations wrapper: %s", body)
	}
	if !strings.Contains(res, "session \"sess-1\"") {
		t.Errorf("result message should mention target session; got %q", res)
	}
}

func TestRenderA2UI_DefaultsToCanvasDefaultSession(t *testing.T) {
	h := newHandler()
	ev, _ := collect(t, h, "agent_render_a2ui",
		`{"ops":[{"version":"v0.9","createSurface":{"surfaceId":"x","catalogId":"y"}}]}`)
	if ev.SessionID != DefaultSession {
		t.Errorf("missing session_id should fall back to %q, got %q", DefaultSession, ev.SessionID)
	}
}

func TestRenderA2UI_RejectsEmptyOps(t *testing.T) {
	h := newHandler()
	_, err := h.HandleToolCall(context.Background(), "agent_render_a2ui",
		json.RawMessage(`{"ops":[]}`))
	if err == nil {
		t.Fatal("expected error for empty ops array")
	}
}

func TestRenderWidget_PublishesEventStateUpdate(t *testing.T) {
	h := newHandler()
	ev, res := collect(t, h, "agent_render_widget",
		`{"widget_id":"dep-graph-1","html":"<svg></svg>","session_id":"sess-2"}`)

	if ev.SessionID != "sess-2" {
		t.Errorf("session_id = %q want sess-2", ev.SessionID)
	}
	var payload iframePayload
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Format != "iframe" {
		t.Errorf("format = %q want iframe", payload.Format)
	}
	if payload.WidgetID != "dep-graph-1" {
		t.Errorf("widget_id = %q want dep-graph-1", payload.WidgetID)
	}
	if payload.HTML != "<svg></svg>" {
		t.Errorf("html not preserved verbatim: %q", payload.HTML)
	}
	if !strings.Contains(res, "dep-graph-1") {
		t.Errorf("result should mention widget id; got %q", res)
	}
}

func TestRenderWidget_RejectsMissingFields(t *testing.T) {
	h := newHandler()
	tests := []struct {
		name  string
		input string
	}{
		{"no widget_id", `{"html":"<p>hi</p>"}`},
		{"no html", `{"widget_id":"x"}`},
		{"empty html", `{"widget_id":"x","html":""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.HandleToolCall(context.Background(), "agent_render_widget", json.RawMessage(tt.input))
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRenderWidget_EnforcesSizeCap(t *testing.T) {
	h := newHandler()
	// Build a payload larger than MaxWidgetHTMLBytes.
	big := strings.Repeat("a", MaxWidgetHTMLBytes+1)
	input, _ := json.Marshal(map[string]any{"widget_id": "x", "html": big})
	_, err := h.HandleToolCall(context.Background(), "agent_render_widget", input)
	if err == nil {
		t.Fatal("expected size-cap error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error should mention exceeds; got %q", err)
	}
}

func TestUnknownTool_Errors(t *testing.T) {
	h := newHandler()
	_, err := h.HandleToolCall(context.Background(), "nope", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestForeignClientName_StashedInOrigin(t *testing.T) {
	// When a foreign MCP client (claude-code, cursor, etc.) calls the
	// tool, mcppkg.AgentClient(ctx) returns the client name. The widget
	// handler stashes it as `origin` so /canvas/ can attribute payloads
	// back to their source agent.
	h := newHandler()
	ctx := mcppkg.WithAgentClient(context.Background(), "claude-code")
	ch, unsub := h.bus.Subscribe(bus.EventStateUpdate)
	defer unsub()

	_, err := h.HandleToolCall(ctx, "agent_render_widget",
		json.RawMessage(`{"widget_id":"x","html":"<p></p>"}`))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		var p iframePayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			t.Fatal(err)
		}
		if p.Origin != "claude-code" {
			t.Errorf("origin = %q want claude-code", p.Origin)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
	}
}
