package lsp

import (
	"fmt"
	"sync"
)

// ServerConfig describes an LSP server configuration.
type ServerConfig struct {
	Language string   `json:"language"`
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
}

// Client wraps an LSP server connection.
type Client struct {
	config ServerConfig
}

// NewClient creates a new LSP client.
func NewClient(config ServerConfig) *Client {
	return &Client{config: config}
}

// ClientRegistry manages LSP clients for different languages.
type ClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewClientRegistry creates a new LSP client registry.
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients: make(map[string]*Client),
	}
}

// Register adds an LSP client for a language.
func (r *ClientRegistry) Register(language string, client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[language] = client
}

// Get returns the LSP client for a language.
func (r *ClientRegistry) Get(language string) (*Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[language]
	return c, ok
}

// Languages returns all registered language names.
func (r *ClientRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	langs := make([]string, 0, len(r.clients))
	for lang := range r.clients {
		langs = append(langs, lang)
	}
	return langs
}

// Hover performs a hover operation.
func (c *Client) Hover(file string, line, col int) (*HoverResult, error) {
	// Stub implementation - would use JSON-RPC 2.0 protocol.
	return nil, fmt.Errorf("LSP hover not yet implemented for %s", c.config.Language)
}

// Definition finds the definition of a symbol.
func (c *Client) Definition(file string, line, col int) ([]Location, error) {
	return nil, fmt.Errorf("LSP definition not yet implemented for %s", c.config.Language)
}

// References finds all references to a symbol.
func (c *Client) References(file string, line, col int) ([]Location, error) {
	return nil, fmt.Errorf("LSP references not yet implemented for %s", c.config.Language)
}

// Symbols lists symbols in a file.
func (c *Client) Symbols(file string) ([]Symbol, error) {
	return nil, fmt.Errorf("LSP symbols not yet implemented for %s", c.config.Language)
}

// Diagnostics returns diagnostics for a file.
func (c *Client) Diagnostics(file string) ([]Diagnostic, error) {
	return nil, fmt.Errorf("LSP diagnostics not yet implemented for %s", c.config.Language)
}
