// Package browsermcp exposes ycode's wire.Action browser automation
// family to foreign MCP-speaking clients (claude-code, opencode,
// codex, gemini-cli, …) over the composite /mcp/ endpoint.
//
// The internal LLM tool registry at internal/tools/browser.go ships
// the same 14 tools to ycode's own runtime. This package wraps the
// same dispatch surface for the public MCP boundary so that everything
// downstream — live/probe/solo backends, the reliability layer — is
// reachable identically through both paths.
package browsermcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/pkg/browser/wire"
)

// MCPHandler dispatches MCP tool calls through a shared browser.Client.
// When client is nil — settings.json browser.mode is unset — every
// tool returns the same friendly "configure browser.mode" message
// rather than a "tool not found" error, so a foreign agent that
// discovers the tools sees actionable guidance.
type MCPHandler struct {
	client browser.Client
}

// NewMCPHandler returns a handler bound to the given client. Pass the
// same client ycode's own runtime uses so probe/solo/live attach state
// is shared across the in-process LLM tools and the MCP boundary.
func NewMCPHandler(client browser.Client) *MCPHandler {
	return &MCPHandler{client: client}
}

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "browser_navigate",
			Description: "Navigate the browser to a URL. Returns page title, current URL, and text content of the body. Requires `browser.mode` in settings.json (live/probe/solo).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"url": {"type": "string"}},
				"required": ["url"]
			}`),
		},
		{
			Name:        "browser_click",
			Description: "Click an element by CSS selector.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"element_id": {"type": "integer"},
					"selector":   {"type": "string"}
				}
			}`),
		},
		{
			Name:        "browser_type",
			Description: "Type text into an input element matched by CSS selector.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"element_id": {"type": "integer"},
					"selector":   {"type": "string"},
					"text":       {"type": "string"}
				},
				"required": ["text"]
			}`),
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll the page up or down by a pixel amount (default 500).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"direction": {"type": "string", "enum": ["up", "down"]},
					"amount":    {"type": "integer"}
				}
			}`),
		},
		{
			Name:        "browser_screenshot",
			Description: "Capture a screenshot of the current page. Returns the PNG as an MCP image content block (rendered inline by clients that support it) plus a text block with title/URL metadata.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_extract",
			Description: "Extract visible text + accessibility info from the current page. Optional `goal` field focuses the extraction (best-effort).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"goal": {"type": "string"}}
			}`),
		},
		{
			Name:        "browser_back",
			Description: "Navigate back one entry in browser history.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_tabs",
			Description: "Manage browser tabs: list (default), switch, new, close.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"tab_action": {"type": "string", "enum": ["list", "switch", "new", "close"]},
					"tab_id":     {"type": "integer"}
				}
			}`),
		},
		{
			Name:        "browser_eval",
			Description: "Evaluate a JavaScript expression in the current page context and return its value. Supported by live, probe, and solo modes (live runs in the page's MAIN world via chrome.scripting). Accepts either an expression (`document.title`) or a statement block (`{ return computeX(); }`); the return value is returned in the `data` field, JSON-stringified for non-string types.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"script": {"type": "string"}},
				"required": ["script"]
			}`),
		},
		{
			Name:        "browser_perf_start",
			Description: "Begin recording a Chrome DevTools performance trace. Pair with browser_perf_stop.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_perf_stop",
			Description: "Stop the active performance trace and return a summary (duration_ms, event_count). Raw events are dropped — use the LCP / paint metrics from browser_lighthouse for page-level perf data.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_network_list",
			Description: "Return the most recent network responses observed since attach (URL, status, MIME type, resource type). Ring buffer caps at 200 entries.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_console_get",
			Description: "Return the most recent console messages + uncaught exceptions observed since attach. Ring buffer caps at 200 entries.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_lighthouse",
			Description: "Return Core Web Vitals + navigation timing from the current page (LCP, CLS, FID, FCP, TTFB, DCL, load, transfer size, resource count). Scope-honest: not full Lighthouse — that needs Node + controlled lab-mode workloads. Navigate first, then call this.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("browsermcp: no resources: %s", uri)
}

// RequiredMode rates every browser tool at DangerFullAccess. The
// posture matches the shell family: browser_eval runs arbitrary JS in
// the page, browser_type can complete logins or submit forms, and
// browser_navigate can reach any URL. Callers that want a stricter
// cap pass `_meta.permission: "ReadOnly"` on the request and the gate
// reduces it; observation-only callers see only browser_screenshot,
// browser_extract, browser_network_list, browser_console_get,
// browser_lighthouse, browser_tabs(list), browser_perf_* — the rest
// reject under that ceiling.
func (h *MCPHandler) RequiredMode(name string) mcp.PermissionMode {
	switch name {
	case "browser_screenshot",
		"browser_extract",
		"browser_network_list",
		"browser_console_get",
		"browser_lighthouse",
		"browser_perf_start",
		"browser_perf_stop":
		return mcp.ModeReadOnly
	case "browser_back",
		"browser_scroll",
		"browser_tabs":
		return mcp.ModeWorkspaceWrite
	default:
		// navigate, click, type, eval — any of these can cause writes
		// the user did not anticipate. Match the shell family's ceiling.
		return mcp.ModeDangerFullAccess
	}
}

// toolToAction maps MCP tool names to the wire.Action discriminator.
// Identity for the eight basic actions; the four DevTools tools are
// renamed with a browser_ prefix so they don't collide with their
// shorter wire names (perf_start, network_list, console_get,
// lighthouse) that the in-process registry uses.
var toolToAction = map[string]string{
	"browser_navigate":     wire.ActionNavigate,
	"browser_click":        wire.ActionClick,
	"browser_type":         wire.ActionType,
	"browser_scroll":       wire.ActionScroll,
	"browser_screenshot":   wire.ActionScreenshot,
	"browser_extract":      wire.ActionExtract,
	"browser_back":         wire.ActionBack,
	"browser_tabs":         wire.ActionTabs,
	"browser_eval":         wire.ActionEvaluate,
	"browser_perf_start":   wire.ActionPerfStart,
	"browser_perf_stop":    wire.ActionPerfStop,
	"browser_network_list": wire.ActionNetworkList,
	"browser_console_get":  wire.ActionConsoleGet,
	"browser_lighthouse":   wire.ActionLighthouse,
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	result, err := h.execute(ctx, name, input)
	if err != nil || result == nil {
		return resultErrToString(err), err
	}
	out, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return "", fmt.Errorf("browsermcp: marshal result: %w", marshalErr)
	}
	return string(out), nil
}

// HandleToolCallRich returns structured MCP content blocks so binary
// payloads survive the wire as their native type. Today only
// browser_screenshot benefits: its PNG ships as an image block (which
// foreign agents render inline) plus a text block carrying title/URL
// metadata. Every other tool falls back to a single text block —
// identical to the legacy HandleToolCall path.
func (h *MCPHandler) HandleToolCallRich(ctx context.Context, name string, input json.RawMessage) ([]mcp.Content, error) {
	// "browser.mode not configured" sentinel: still a text block, but
	// surface it through the same path so the message stays uniform.
	if h.client == nil {
		return []mcp.Content{mcp.ContentText(unconfiguredMessage)}, nil
	}
	result, err := h.execute(ctx, name, input)
	if err != nil {
		return nil, err
	}
	if name == "browser_screenshot" && result != nil && result.Image != "" {
		// MIME is fixed at the live extension and the probe/solo
		// drivers: chrome.tabs.captureVisibleTab and chromedp's
		// CaptureScreenshot both produce PNG by default.
		blocks := []mcp.Content{mcp.ContentImage(result.Image, "image/png")}
		if meta := screenshotMetadata(result); meta != "" {
			blocks = append(blocks, mcp.ContentText(meta))
		}
		return blocks, nil
	}
	out, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return nil, fmt.Errorf("browsermcp: marshal result: %w", marshalErr)
	}
	return []mcp.Content{mcp.ContentText(string(out))}, nil
}

// execute is the shared dispatch path used by both HandleToolCall and
// HandleToolCallRich. It returns a *wire.Result on success (or nil
// when the client isn't configured) and never auto-marshals — callers
// decide on the output shape.
func (h *MCPHandler) execute(ctx context.Context, name string, input json.RawMessage) (*wire.Result, error) {
	actionType, ok := toolToAction[name]
	if !ok {
		return nil, fmt.Errorf("browsermcp: unknown tool %q", name)
	}
	if h.client == nil {
		return nil, nil
	}
	var action wire.Action
	if len(input) > 0 {
		if err := json.Unmarshal(input, &action); err != nil {
			return nil, fmt.Errorf("browsermcp: parse input: %w", err)
		}
	}
	action.Type = actionType
	result, err := h.client.Execute(ctx, action)
	if err != nil {
		return nil, fmt.Errorf("browsermcp: %s: %w", name, err)
	}
	return result, nil
}

const unconfiguredMessage = "Browser tools are not available: no `browser.mode` configured. " +
	"Set `browser.mode` to live, probe, or solo in settings.json " +
	"(see `ycode browser doctor` for readiness)."

// resultErrToString preserves the legacy "client nil → friendly text,
// no error" contract used by HandleToolCall.
func resultErrToString(err error) string {
	if err != nil {
		return ""
	}
	return unconfiguredMessage
}

// screenshotMetadata is a compact one-liner that travels alongside the
// PNG so the agent still sees the page title and URL it captured.
// Empty when the backend supplied neither.
func screenshotMetadata(r *wire.Result) string {
	switch {
	case r.Title != "" && r.URL != "":
		return fmt.Sprintf("screenshot: %s (%s)", r.Title, r.URL)
	case r.URL != "":
		return "screenshot: " + r.URL
	case r.Title != "":
		return "screenshot: " + r.Title
	}
	return ""
}
