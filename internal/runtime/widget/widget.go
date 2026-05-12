// Package widget exposes ycode's generative-UI surface as MCP tools.
//
// Two tools, both foreign-callable (claude-code, opencode, codex, etc.
// drive ycode's /canvas/ via the same endpoint ycode's own agent uses):
//
//   - agent_render_a2ui    — emits A2UI v0.9 ops (createSurface /
//     updateComponents / updateDataModel). Use for declarative,
//     structured surfaces with bidirectional state (todo kanban,
//     service health, memo clusters). Bound to a SurfaceID.
//
//   - agent_render_widget  — emits raw HTML for a sandboxed iframe.
//     Use for generative answer widgets that need expressiveness the
//     A2UI catalog can't reach (ad-hoc dashboards, dep graphs, trace
//     viewers, custom visualizations). Bound to a WidgetID.
//
// Both tools publish a single bus.EventStateUpdate carrying the
// payload + a format discriminator. ycode's /canvas/ (or any other
// subscriber on the session) renders it. SessionID identifies which
// /canvas/ tab receives the payload; absence falls back to the
// well-known "canvas-default" session.
package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	mcppkg "github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/a2ui"
)

// DefaultSession is the well-known session ID used when a caller
// (especially a foreign agent that hasn't been handed a session by
// the user) doesn't specify one. The /canvas/ route subscribes to
// this session by default, so the trivial-case round-trip works
// without explicit session plumbing.
const DefaultSession = "canvas-default"

// MaxWidgetHTMLBytes caps a single agent_render_widget payload. Strong
// models occasionally over-produce; this prevents a runaway emission
// from bloating the bus / WS. Iframes that need more can be split into
// multiple chunks (future) or use A2UI ops instead.
const MaxWidgetHTMLBytes = 256 * 1024

// MCPHandler is the ServerHandler that exposes the two widget tools.
//
// The handler is stateless except for the bus reference — every call
// translates input into a single EventStateUpdate publish. Construct
// once per ycode serve invocation and register on the composite MCP
// endpoint alongside the other capability families.
type MCPHandler struct {
	bus bus.Bus
}

// NewMCPHandler returns a handler that publishes widget/A2UI payloads
// onto the given bus. Callers wire it into the composite MCP handler
// in cmd/ycode/serve.go alongside treesitter, skills, shell, etc.
func NewMCPHandler(b bus.Bus) *MCPHandler {
	return &MCPHandler{bus: b}
}

// --- MCP plumbing ----------------------------------------------------------

func (h *MCPHandler) ListResources() []mcppkg.Resource { return nil }

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("widget: no resources: %s", uri)
}

// RequiredMode classifies both tools as read-only — they publish
// structured display payloads onto the bus; they do not touch the
// filesystem, run commands, or call out. The rendered iframe widgets
// can request user gestures back, but those are bounded by the
// sandboxed-iframe + postMessage channel, not by this tool's mode.
func (h *MCPHandler) RequiredMode(_ string) mcppkg.PermissionMode {
	return mcppkg.ModeReadOnly
}

func (h *MCPHandler) ListTools() []mcppkg.Tool {
	return []mcppkg.Tool{
		{
			Name: "agent_render_a2ui",
			Description: "Render or update a structured UI surface on the user's canvas using A2UI v0.9 ops. " +
				"Use for declarative components with bidirectional state — todo boards, dashboards with selectable rows, " +
				"forms, lists of cards. Surface IDs are stable handles: emit createSurface once, then stream " +
				"updateComponents (to change the UI) and updateDataModel (to push new data) against the same surfaceId. " +
				"For free-form HTML/SVG visualizations, use agent_render_widget instead.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"ops": {
						"type": "array",
						"description": "A2UI v0.9 op array. Each op is one of: {version, createSurface:{surfaceId,catalogId}}, {version, updateComponents:{surfaceId,components}}, {version, updateDataModel:{surfaceId,path,value}}.",
						"items": {"type": "object"}
					},
					"session_id": {
						"type": "string",
						"description": "Optional. Target canvas session. Defaults to 'canvas-default' — the session /canvas/ subscribes to with no ?session= query param."
					}
				},
				"required": ["ops"]
			}`),
		},
		{
			Name: "agent_render_widget",
			Description: "Render a free-form generative widget (HTML/SVG/Chart.js/D3/Three.js — anything browser-renderable) into a sandboxed iframe on the user's canvas. " +
				"Use when the visual answer needs expressiveness the A2UI catalog can't reach: ad-hoc dashboards with custom chart layouts, " +
				"dependency graphs, animated trace visualizations, flamegraphs, schema diagrams. For structured + bidirectional surfaces (todo board, " +
				"form, selectable list), use agent_render_a2ui instead. The widget runs sandboxed; postMessage is the only outbound channel " +
				"(use it to send back user-gesture events like 'accept hunk #3' or 'drill into node X').",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"widget_id": {
						"type": "string",
						"description": "Stable widget identifier. Re-using the same widget_id replaces the existing widget; a fresh ID creates a new one."
					},
					"html": {
						"type": "string",
						"description": "Standalone HTML for the widget body. Will be wrapped with the canvas bridge.js (postMessage + ResizeObserver) before being injected into a sandboxed iframe. May import via CDN (Chart.js, D3, Three.js, etc.)."
					},
					"session_id": {
						"type": "string",
						"description": "Optional. Defaults to 'canvas-default'."
					}
				},
				"required": ["widget_id", "html"]
			}`),
		},
	}
}

// --- handlers --------------------------------------------------------------

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "agent_render_a2ui":
		return h.handleRenderA2UI(ctx, input)
	case "agent_render_widget":
		return h.handleRenderWidget(ctx, input)
	default:
		return "", fmt.Errorf("unknown widget tool: %s", name)
	}
}

// a2uiPayload is the inner Data of an EventStateUpdate with format="a2ui".
// Wraps a batch of ops in the v0.9 OperationsKey container so renderers
// can validate-and-route by looking for that key.
type a2uiPayload struct {
	Format string          `json:"format"`         // "a2ui"
	Body   json.RawMessage `json:"body"`           // {"a2ui_operations": [...]}
	Origin string          `json:"origin,omitempty"` // foreign agent name, if known
}

func (h *MCPHandler) handleRenderA2UI(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Ops       []a2ui.Op `json:"ops"`
		SessionID string    `json:"session_id"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("agent_render_a2ui: %w", err)
	}
	if len(args.Ops) == 0 {
		return "", fmt.Errorf("agent_render_a2ui: at least one op required")
	}
	body, err := a2ui.Render(args.Ops)
	if err != nil {
		return "", fmt.Errorf("agent_render_a2ui: render: %w", err)
	}

	payload, err := json.Marshal(a2uiPayload{
		Format: "a2ui",
		Body:   body,
		Origin: mcppkg.AgentClient(ctx),
	})
	if err != nil {
		return "", fmt.Errorf("agent_render_a2ui: marshal: %w", err)
	}

	h.bus.Publish(bus.Event{
		Type:      bus.EventStateUpdate,
		SessionID: orDefault(args.SessionID, DefaultSession),
		Timestamp: time.Now(),
		Data:      payload,
	})

	return fmt.Sprintf("Rendered %d A2UI op(s) on session %q.",
		len(args.Ops), orDefault(args.SessionID, DefaultSession)), nil
}

// iframePayload is the inner Data of an EventStateUpdate with format="iframe".
// HTML is verbatim — the canvas-side bridge wraps it with the bridge.js
// shim that supplies postMessage + ResizeObserver behavior.
type iframePayload struct {
	Format   string `json:"format"`             // "iframe"
	WidgetID string `json:"widget_id"`
	HTML     string `json:"html"`
	Origin   string `json:"origin,omitempty"`
}

func (h *MCPHandler) handleRenderWidget(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		WidgetID  string `json:"widget_id"`
		HTML      string `json:"html"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("agent_render_widget: %w", err)
	}
	if args.WidgetID == "" {
		return "", fmt.Errorf("agent_render_widget: widget_id is required")
	}
	if args.HTML == "" {
		return "", fmt.Errorf("agent_render_widget: html is required")
	}
	if len(args.HTML) > MaxWidgetHTMLBytes {
		return "", fmt.Errorf("agent_render_widget: html exceeds %d bytes (got %d) — split into smaller widgets or use agent_render_a2ui",
			MaxWidgetHTMLBytes, len(args.HTML))
	}

	payload, err := json.Marshal(iframePayload{
		Format:   "iframe",
		WidgetID: args.WidgetID,
		HTML:     args.HTML,
		Origin:   mcppkg.AgentClient(ctx),
	})
	if err != nil {
		return "", fmt.Errorf("agent_render_widget: marshal: %w", err)
	}

	h.bus.Publish(bus.Event{
		Type:      bus.EventStateUpdate,
		SessionID: orDefault(args.SessionID, DefaultSession),
		Timestamp: time.Now(),
		Data:      payload,
	})

	return fmt.Sprintf("Rendered widget %q on session %q (%d bytes).",
		args.WidgetID, orDefault(args.SessionID, DefaultSession), len(args.HTML)), nil
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
