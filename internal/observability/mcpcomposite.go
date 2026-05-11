package observability

import (
	"context"
	"net/http"
	"sync/atomic"
)

// MCPCompositeComponent serves the single composite MCP endpoint mounted at
// /mcp/. It fans out tool calls to every registered capability family
// (gitea, loom, pulse, and future memex/repomap/inference) so foreign
// clients — claude code, opencode, codex, gemini-cli, ycode's own TUI —
// configure ONE MCP entry instead of one per family.
//
// Construction is identical to MCPComponent; only the Name() differs so
// the stack manager routes it to /mcp/ via componentPathMap.
type MCPCompositeComponent struct {
	handler http.Handler
	healthy atomic.Bool
}

// NewMCPCompositeComponent creates a component serving the composite MCP
// endpoint. handler is the http.Handler produced by NewMCPHTTPHandler
// wrapping an mcp.CompositeHandler.
func NewMCPCompositeComponent(handler http.Handler) *MCPCompositeComponent {
	return &MCPCompositeComponent{handler: handler}
}

func (c *MCPCompositeComponent) Name() string { return "ycode-mcp" }

func (c *MCPCompositeComponent) Start(_ context.Context) error {
	c.healthy.Store(true)
	return nil
}

func (c *MCPCompositeComponent) Stop(_ context.Context) error {
	c.healthy.Store(false)
	return nil
}

func (c *MCPCompositeComponent) Healthy() bool {
	return c.healthy.Load()
}

func (c *MCPCompositeComponent) HTTPHandler() http.Handler {
	return c.handler
}
