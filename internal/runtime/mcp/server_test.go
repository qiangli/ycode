package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// captureHandler is a minimal ServerHandler that records the ctx of
// the last HandleToolCall — used to assert that the MCP server seeds
// the call context with WithAgentClient.
type captureHandler struct {
	gotCtx context.Context
}

func (h *captureHandler) HandleToolCall(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
	h.gotCtx = ctx
	return "ok", nil
}
func (h *captureHandler) ListTools() []Tool                                    { return nil }
func (h *captureHandler) ListResources() []Resource                            { return nil }
func (h *captureHandler) ReadResource(context.Context, string) (string, error) { return "", nil }

func TestServer_CapturesClientInfoFromInitialize(t *testing.T) {
	h := &captureHandler{}
	s := NewServer(h)

	// Step 1: initialize with clientInfo.
	initReq := &JSONRPCRequest{
		ID:     1,
		Method: "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "claude-code",
				"version": "0.2.3",
			},
		},
	}
	if _, err := s.HandleRequest(context.Background(), initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if got := s.ClientName(); got != "claude-code" {
		t.Fatalf("ClientName() = %q; want claude-code", got)
	}

	// Step 2: tools/call should seed ctx with the captured name.
	callReq := &JSONRPCRequest{
		ID:     2,
		Method: "tools/call",
		Params: map[string]any{"name": "any-tool", "arguments": map[string]any{}},
	}
	if _, err := s.HandleRequest(context.Background(), callReq); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if got := AgentClient(h.gotCtx); got != "claude-code" {
		t.Fatalf("AgentClient(ctx) = %q; want claude-code", got)
	}
}

func TestServer_NoClientInfo(t *testing.T) {
	h := &captureHandler{}
	s := NewServer(h)

	// initialize with no clientInfo field.
	initReq := &JSONRPCRequest{
		ID:     1,
		Method: "initialize",
		Params: map[string]any{"protocolVersion": "2024-11-05"},
	}
	if _, err := s.HandleRequest(context.Background(), initReq); err != nil {
		t.Fatal(err)
	}
	if got := s.ClientName(); got != "" {
		t.Fatalf("ClientName() = %q; want empty", got)
	}

	callReq := &JSONRPCRequest{
		ID:     2,
		Method: "tools/call",
		Params: map[string]any{"name": "any-tool", "arguments": map[string]any{}},
	}
	if _, err := s.HandleRequest(context.Background(), callReq); err != nil {
		t.Fatal(err)
	}
	if got := AgentClient(h.gotCtx); got != "" {
		t.Fatalf("AgentClient(ctx) = %q; want empty (no clientInfo)", got)
	}
}

func TestWithAgentClient_EmptyIsNoop(t *testing.T) {
	ctx := WithAgentClient(context.Background(), "")
	if got := AgentClient(ctx); got != "" {
		t.Fatalf("AgentClient = %q; want empty", got)
	}
}

// richHandler exercises the RichHandler path: tools/call routed through
// a handler that returns image + text content blocks should land on the
// wire as a two-element content array, not a single stringified JSON.
type richHandler struct{}

func (richHandler) HandleToolCall(context.Context, string, json.RawMessage) (string, error) {
	return "fallback-text", nil
}
func (richHandler) HandleToolCallRich(_ context.Context, name string, _ json.RawMessage) ([]Content, error) {
	if name == "screenshot" {
		return []Content{
			ContentImage("aGVsbG8=", "image/png"),
			ContentText("meta"),
		}, nil
	}
	return []Content{ContentText("plain")}, nil
}
func (richHandler) ListTools() []Tool                                    { return nil }
func (richHandler) ListResources() []Resource                            { return nil }
func (richHandler) ReadResource(context.Context, string) (string, error) { return "", nil }

func TestServer_RichHandlerEmitsImageContent(t *testing.T) {
	s := NewServer(richHandler{})
	resp, err := s.HandleRequest(context.Background(), &JSONRPCRequest{
		ID:     1,
		Method: "tools/call",
		Params: map[string]any{"name": "screenshot", "arguments": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}
	var parsed struct {
		Content []Content `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v\nraw: %s", err, resp.Result)
	}
	if len(parsed.Content) != 2 {
		t.Fatalf("content len = %d; want 2 (image+text)", len(parsed.Content))
	}
	if parsed.Content[0].Type != "image" {
		t.Fatalf("block[0].Type = %q; want image", parsed.Content[0].Type)
	}
	if parsed.Content[0].Data != "aGVsbG8=" || parsed.Content[0].MimeType != "image/png" {
		t.Fatalf("block[0] = %+v; want data=aGVsbG8= mime=image/png", parsed.Content[0])
	}
	if parsed.Content[0].Text != "" {
		t.Fatalf("image block must not carry Text; got %q", parsed.Content[0].Text)
	}
	if parsed.Content[1].Type != "text" || parsed.Content[1].Text != "meta" {
		t.Fatalf("block[1] = %+v; want type=text text=meta", parsed.Content[1])
	}
}

// TestServer_LegacyHandlerWrapsInTextBlock ensures handlers that do
// NOT implement RichHandler keep producing a single text content
// block — the wire-compat guarantee for every existing handler that
// returns plain strings.
func TestServer_LegacyHandlerWrapsInTextBlock(t *testing.T) {
	s := NewServer(&captureHandler{})
	resp, err := s.HandleRequest(context.Background(), &JSONRPCRequest{
		ID:     1,
		Method: "tools/call",
		Params: map[string]any{"name": "anything", "arguments": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var parsed struct {
		Content []Content `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v\nraw: %s", err, resp.Result)
	}
	if len(parsed.Content) != 1 || parsed.Content[0].Type != "text" || parsed.Content[0].Text != "ok" {
		t.Fatalf("content = %+v; want single text block 'ok'", parsed.Content)
	}
}
