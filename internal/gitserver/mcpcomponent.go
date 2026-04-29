package gitserver

import (
	"context"
	"net/http"
	"sync/atomic"
)

// GiteaMCPComponent wraps the Gitea MCP server as an observability stack
// component, mountable on the reverse proxy at /gitea-mcp/.
// It implements the Component interface (Name, Start, Stop, Healthy, HTTPHandler).
type GiteaMCPComponent struct {
	handler http.Handler
	healthy atomic.Bool
}

// NewGiteaMCPComponent creates a component serving the Gitea MCP server.
func NewGiteaMCPComponent(handler http.Handler) *GiteaMCPComponent {
	return &GiteaMCPComponent{handler: handler}
}

func (c *GiteaMCPComponent) Name() string { return "gitea-mcp" }

func (c *GiteaMCPComponent) Start(_ context.Context) error {
	c.healthy.Store(true)
	return nil
}

func (c *GiteaMCPComponent) Stop(_ context.Context) error {
	c.healthy.Store(false)
	return nil
}

func (c *GiteaMCPComponent) Healthy() bool {
	return c.healthy.Load()
}

// HTTPHandler returns the MCP HTTP handler for in-process mounting on the proxy.
func (c *GiteaMCPComponent) HTTPHandler() http.Handler {
	return c.handler
}
