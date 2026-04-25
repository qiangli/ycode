package observability

import (
	"context"
	"net/http"
	"sync/atomic"
)

// MCPComponent wraps the Pulse MCP server as an observability stack
// component, mountable on the reverse proxy at /pulse/.
// Pulse is ycode's observability hub — traces, metrics, logs, dashboards,
// alerts, all controllable via MCP protocol.
type MCPComponent struct {
	handler http.Handler
	healthy atomic.Bool
}

// NewMCPComponent creates a component serving the Pulse MCP server.
func NewMCPComponent(handler http.Handler) *MCPComponent {
	return &MCPComponent{handler: handler}
}

func (c *MCPComponent) Name() string { return "pulse" }

func (c *MCPComponent) Start(ctx context.Context) error {
	c.healthy.Store(true)
	return nil
}

func (c *MCPComponent) Stop(ctx context.Context) error {
	c.healthy.Store(false)
	return nil
}

func (c *MCPComponent) Healthy() bool {
	return c.healthy.Load()
}

// HTTPHandler returns the MCP HTTP handler for in-process mounting on the proxy.
func (c *MCPComponent) HTTPHandler() http.Handler {
	return c.handler
}
