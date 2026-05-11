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
func (h *captureHandler) ListTools() []Tool               { return nil }
func (h *captureHandler) ListResources() []Resource       { return nil }
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
