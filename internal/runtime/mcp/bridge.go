package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Bridge connects MCP tools to the ycode tool registry.
type Bridge struct {
	registry *Registry
}

// NewBridge creates a new MCP tool bridge.
func NewBridge(registry *Registry) *Bridge {
	return &Bridge{registry: registry}
}

// DiscoverTools connects to all configured servers and returns their tools.
func (b *Bridge) DiscoverTools(ctx context.Context) ([]BridgedTool, error) {
	var tools []BridgedTool

	b.registry.mu.RLock()
	clients := make(map[string]*Client, len(b.registry.clients))
	for name, client := range b.registry.clients {
		clients[name] = client
	}
	b.registry.mu.RUnlock()

	for serverName, client := range clients {
		serverTools := client.ListTools()
		for _, tool := range serverTools {
			normalizedName := NormalizeName(serverName, tool.Name)
			tools = append(tools, BridgedTool{
				NormalizedName: normalizedName,
				OriginalName:   tool.Name,
				ServerName:     serverName,
				Description:    tool.Description,
				InputSchema:    tool.InputSchema,
			})
		}
	}

	return tools, nil
}

// BridgedTool represents an MCP tool exposed via the bridge.
type BridgedTool struct {
	NormalizedName string          `json:"normalized_name"` // mcp__{server}__{tool}
	OriginalName   string          `json:"original_name"`
	ServerName     string          `json:"server_name"`
	Description    string          `json:"description"`
	InputSchema    json.RawMessage `json:"input_schema"`
}

// CallBridgedTool invokes a tool through the bridge using its normalized name.
func (b *Bridge) CallBridgedTool(ctx context.Context, normalizedName string, input json.RawMessage) (string, error) {
	serverName, toolName, err := ParseNormalizedName(normalizedName)
	if err != nil {
		return "", err
	}

	client, ok := b.registry.Get(serverName)
	if !ok {
		return "", fmt.Errorf("MCP server %q not found", serverName)
	}

	return client.CallTool(ctx, toolName, input)
}

// ParseNormalizedName splits mcp__{server}__{tool} into server and tool.
func ParseNormalizedName(name string) (server, tool string, err error) {
	if !strings.HasPrefix(name, "mcp__") {
		return "", "", fmt.Errorf("not an MCP tool name: %s", name)
	}

	rest := name[5:] // remove "mcp__"
	idx := strings.Index(rest, "__")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid MCP tool name format: %s", name)
	}

	return rest[:idx], rest[idx+2:], nil
}
