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
	InputSchema json.RawMessage `json:"input_schema"`
	ServerName  string          `json:"server_name"`
}

// Resource represents an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// ServerConfig describes how to connect to an MCP server.
type ServerConfig struct {
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport"` // stdio, sse
}

// Client wraps a connection to an MCP server.
type Client struct {
	config ServerConfig
	tools  []Tool
}

// NewClient creates a new MCP client.
func NewClient(config ServerConfig) *Client {
	return &Client{config: config}
}

// Connect initializes the connection and discovers tools.
func (c *Client) Connect(ctx context.Context) error {
	// Stub: would spawn process, init JSON-RPC, call tools/list.
	return fmt.Errorf("MCP client connection not yet implemented")
}

// ListTools returns discovered tools.
func (c *Client) ListTools() []Tool {
	return c.tools
}

// CallTool invokes an MCP tool.
func (c *Client) CallTool(ctx context.Context, name string, input json.RawMessage) (string, error) {
	return "", fmt.Errorf("MCP tool call not yet implemented")
}

// ListResources returns MCP resources.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	return nil, fmt.Errorf("MCP list resources not yet implemented")
}

// ReadResource reads an MCP resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	return "", fmt.Errorf("MCP read resource not yet implemented")
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

// NormalizeName creates the mcp__{server}__{tool} name format.
func NormalizeName(server, tool string) string {
	return fmt.Sprintf("mcp__%s__%s", server, tool)
}
