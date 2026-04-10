package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// MCPToolDeps holds dependencies for MCP tool handlers.
type MCPToolDeps struct {
	CallTool      func(ctx context.Context, name string, input json.RawMessage) (string, error)
	ListResources func(ctx context.Context, serverName string) (string, error)
	ReadResource  func(ctx context.Context, serverName, uri string) (string, error)
	Authenticate  func(ctx context.Context, serverName string) (string, error)
}

// RegisterMCPHandlers registers MCP tool handlers.
func RegisterMCPHandlers(r *Registry, deps *MCPToolDeps) {
	if spec, ok := r.Get("MCP"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				ServerName string          `json:"server_name"`
				ToolName   string          `json:"tool_name"`
				Arguments  json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse MCP input: %w", err)
			}
			normalizedName := fmt.Sprintf("mcp__%s__%s", params.ServerName, params.ToolName)
			if deps.CallTool != nil {
				return deps.CallTool(ctx, normalizedName, params.Arguments)
			}
			return "", fmt.Errorf("MCP tool call not configured")
		}
	}

	if spec, ok := r.Get("ListMcpResources"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				ServerName string `json:"server_name"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse ListMcpResources input: %w", err)
			}
			if deps.ListResources != nil {
				return deps.ListResources(ctx, params.ServerName)
			}
			return "No resources available.", nil
		}
	}

	if spec, ok := r.Get("ReadMcpResource"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				ServerName string `json:"server_name"`
				URI        string `json:"uri"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse ReadMcpResource input: %w", err)
			}
			if deps.ReadResource != nil {
				return deps.ReadResource(ctx, params.ServerName, params.URI)
			}
			return "", fmt.Errorf("MCP resource reading not configured")
		}
	}

	if spec, ok := r.Get("McpAuth"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				ServerName string `json:"server_name"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse McpAuth input: %w", err)
			}
			if deps.Authenticate != nil {
				return deps.Authenticate(ctx, params.ServerName)
			}
			return "", fmt.Errorf("MCP authentication not configured")
		}
	}
}
