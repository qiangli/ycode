package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Tool represents an MCP tool discovered from a server.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	ServerName  string          `json:"server_name"`
}

// Resource represents an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ServerConfig describes how to connect to an MCP server.
type ServerConfig struct {
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport"` // "stdio" (default) or "sse"
}

// Client wraps a connection to an MCP server.
type Client struct {
	config    ServerConfig
	transport *StdioTransport
	tools     []Tool
	mu        sync.Mutex
}

// NewClient creates a new MCP client.
func NewClient(config ServerConfig) *Client {
	return &Client{config: config}
}

// Connect initializes the connection: spawns the server process,
// performs the MCP initialize handshake, and discovers available tools.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport != nil {
		return nil // already connected
	}

	transport, err := NewStdioTransport(c.config.Command, c.config.Args, c.config.Env)
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}

	if err := transport.Start(); err != nil {
		return fmt.Errorf("start server process %q: %w", c.config.Command, err)
	}

	// MCP initialize handshake.
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "ycode",
			"version": "0.1.0",
		},
	}

	_, err = transport.Call(ctx, "initialize", initParams)
	if err != nil {
		transport.Close()
		return fmt.Errorf("initialize handshake: %w", err)
	}

	// Send initialized notification (no response expected).
	if err := transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		transport.Close()
		return fmt.Errorf("send initialized notification: %w", err)
	}

	// Discover tools.
	result, err := transport.Call(ctx, "tools/list", nil)
	if err != nil {
		// tools/list is optional; some servers may not have tools.
		c.transport = transport
		return nil
	}

	var toolsResult struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolsResult); err != nil {
		transport.Close()
		return fmt.Errorf("parse tools/list response: %w", err)
	}

	for i := range toolsResult.Tools {
		toolsResult.Tools[i].ServerName = c.config.Name
	}
	c.tools = toolsResult.Tools
	c.transport = transport
	return nil
}

// ListTools returns discovered tools.
func (c *Client) ListTools() []Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools
}

// CallTool invokes an MCP tool.
func (c *Client) CallTool(ctx context.Context, name string, input json.RawMessage) (string, error) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()

	if transport == nil {
		return "", fmt.Errorf("MCP client not connected to server %q", c.config.Name)
	}

	params := map[string]any{
		"name":      name,
		"arguments": json.RawMessage(input),
	}

	result, err := transport.Call(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("tools/call %q: %w", name, err)
	}

	// Parse the tool result content.
	var toolResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(result, &toolResult); err != nil {
		// If we can't parse the structured result, return raw JSON.
		return string(result), nil
	}

	// Concatenate text content blocks.
	var text string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			if text != "" {
				text += "\n"
			}
			text += c.Text
		}
	}

	if toolResult.IsError {
		return "", fmt.Errorf("MCP tool error: %s", text)
	}

	return text, nil
}

// ListResources returns MCP resources from the server.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()

	if transport == nil {
		return nil, fmt.Errorf("MCP client not connected to server %q", c.config.Name)
	}

	result, err := transport.Call(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("resources/list: %w", err)
	}

	var resourcesResult struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(result, &resourcesResult); err != nil {
		return nil, fmt.Errorf("parse resources/list response: %w", err)
	}

	return resourcesResult.Resources, nil
}

// ReadResource reads an MCP resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()

	if transport == nil {
		return "", fmt.Errorf("MCP client not connected to server %q", c.config.Name)
	}

	params := map[string]string{"uri": uri}
	result, err := transport.Call(ctx, "resources/read", params)
	if err != nil {
		return "", fmt.Errorf("resources/read %q: %w", uri, err)
	}

	var readResult struct {
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(result, &readResult); err != nil {
		return string(result), nil
	}

	if len(readResult.Contents) > 0 {
		return readResult.Contents[0].Text, nil
	}
	return "", nil
}

// Close shuts down the MCP client and its server process.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport == nil {
		return nil
	}

	err := c.transport.Close()
	c.transport = nil
	c.tools = nil
	return err
}

// Registry manages multiple MCP server connections.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewRegistry creates a new MCP registry.
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*Client),
	}
}

// Add registers an MCP client.
func (r *Registry) Add(name string, client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[name] = client
}

// Get returns an MCP client by server name.
func (r *Registry) Get(name string) (*Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	return c, ok
}

// All returns all registered clients.
func (r *Registry) All() map[string]*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]*Client, len(r.clients))
	for k, v := range r.clients {
		result[k] = v
	}
	return result
}

// Close shuts down all MCP clients.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, client := range r.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close MCP client %q: %w", name, err)
		}
	}
	r.clients = make(map[string]*Client)
	return firstErr
}

// NormalizeName creates the mcp__{server}__{tool} name format.
func NormalizeName(server, tool string) string {
	return fmt.Sprintf("mcp__%s__%s", server, tool)
}
