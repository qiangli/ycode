package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// CompositeHandler aggregates multiple ServerHandler implementations into one,
// routing tools/call by tool name and resources/read by URI prefix to the
// sub-handler that registered the entry. This is how capability families
// (AST, repo-map, memex, inference, workspaces, ...) plug into a single
// `ycode mcp serve` surface without one monolithic handler.
//
// Construction is one-shot: tools and resources are snapshotted at New time.
// If a sub-handler's tool list changes at runtime, build a new composite.
type CompositeHandler struct {
	tools         []Tool
	resources     []Resource
	toolHandlers  map[string]ServerHandler
	resourceOwner map[string]ServerHandler
}

// NewCompositeHandler aggregates the given handlers. The first handler that
// declares a given tool name (or resource URI) wins; later duplicates are
// dropped with a panic at construction so collisions surface immediately.
func NewCompositeHandler(handlers ...ServerHandler) *CompositeHandler {
	c := &CompositeHandler{
		tools:         []Tool{},
		resources:     []Resource{},
		toolHandlers:  make(map[string]ServerHandler),
		resourceOwner: make(map[string]ServerHandler),
	}
	for _, h := range handlers {
		for _, t := range h.ListTools() {
			if _, dup := c.toolHandlers[t.Name]; dup {
				panic(fmt.Sprintf("mcp.CompositeHandler: duplicate tool %q", t.Name))
			}
			c.toolHandlers[t.Name] = h
			c.tools = append(c.tools, t)
		}
		for _, r := range h.ListResources() {
			if _, dup := c.resourceOwner[r.URI]; dup {
				panic(fmt.Sprintf("mcp.CompositeHandler: duplicate resource %q", r.URI))
			}
			c.resourceOwner[r.URI] = h
			c.resources = append(c.resources, r)
		}
	}
	return c
}

func (c *CompositeHandler) ListTools() []Tool         { return c.tools }
func (c *CompositeHandler) ListResources() []Resource { return c.resources }

func (c *CompositeHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	h, ok := c.toolHandlers[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return h.HandleToolCall(ctx, name, input)
}

func (c *CompositeHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	h, ok := c.resourceOwner[uri]
	if !ok {
		return "", fmt.Errorf("unknown resource: %s", uri)
	}
	return h.ReadResource(ctx, uri)
}

// RequiredMode delegates to the sub-handler that registered toolName, so a
// GatedHandler wrapping the composite sees per-tool permission requirements
// rather than treating the whole composite as a single mode. Sub-handlers
// that do not implement PermissionAware are treated as ReadOnly — capability
// families exposing write-capable tools must implement the interface.
func (c *CompositeHandler) RequiredMode(toolName string) PermissionMode {
	h, ok := c.toolHandlers[toolName]
	if !ok {
		return ModeReadOnly
	}
	if pa, ok := h.(PermissionAware); ok {
		return pa.RequiredMode(toolName)
	}
	return ModeReadOnly
}
