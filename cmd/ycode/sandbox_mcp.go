package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

type MCPHandler struct{}

func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{{
		Name:        "sandbox_exec",
		Description: "Sandbox execution is delegated outside ycode in this lean build. Run ycode under bashy or another host-layer sandbox instead.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "array", "items": {"type": "string"}},
				"workdir": {"type": "string"},
				"timeout_ms": {"type": "integer"}
			},
			"required": ["command"]
		}`),
	}}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeDangerFullAccess
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if name != "sandbox_exec" {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return "", fmt.Errorf("sandbox_exec is not available in lean ycode; delegate sandboxing to bashy or another external wrapper")
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}
