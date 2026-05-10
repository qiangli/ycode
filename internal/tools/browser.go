package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// BrowserAction is the wire-level browser action shared between the
// tool shim and whichever mcpservers backend is installed. Defined
// here (rather than imported from internal/runtime/mcpservers) so the
// stable build, which excludes the experimental browser package,
// still compiles. The experimental wiring
// (internal/tools/browser_experimental.go) copies fields one-for-one
// into mcpservers.BrowserAction.
type BrowserAction struct {
	Type      string   `json:"action"`
	URL       string   `json:"url,omitempty"`
	ElementID int      `json:"element_id,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	Text      string   `json:"text,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Amount    int      `json:"amount,omitempty"`
	Goal      string   `json:"goal,omitempty"`
	TabID     int      `json:"tab_id,omitempty"`
	TabAction string   `json:"tab_action,omitempty"`
	Script    string   `json:"script,omitempty"`
	URLs      []string `json:"urls,omitempty"`
}

// BrowserResult mirrors mcpservers.BrowserResult. See note on
// BrowserAction.
type BrowserResult struct {
	Success      bool     `json:"success"`
	Title        string   `json:"title,omitempty"`
	URL          string   `json:"url,omitempty"`
	Content      string   `json:"content,omitempty"`
	Elements     string   `json:"elements,omitempty"`
	Data         string   `json:"data,omitempty"`
	Image        string   `json:"image,omitempty"`
	Error        string   `json:"error,omitempty"`
	Hints        []string `json:"hints,omitempty"`
	OutcomeClass string   `json:"outcome_class,omitempty"`
}

// browserDispatchHook is installed by the experimental wiring
// (SetBrowserManager in internal/tools/browser_experimental.go). When
// nil, browser_* tools report "no browser backend configured."
var browserDispatchHook func(ctx context.Context, action BrowserAction) (*BrowserResult, error)

// SetBrowserDispatchHook installs a backend for the lifetime of the
// session. Pass nil to clear.
func SetBrowserDispatchHook(fn func(ctx context.Context, action BrowserAction) (*BrowserResult, error)) {
	browserDispatchHook = fn
}

// RegisterBrowserHandlers registers the browser automation tools. The
// 8 browser_* tools share one dispatch path; mode-specific actions
// (perf_start, network_list, …) are dispatched by name inside the
// experimental backend.
func RegisterBrowserHandlers(r *Registry) {
	registerBrowserNavigate(r)
	registerBrowserClick(r)
	registerBrowserType(r)
	registerBrowserScroll(r)
	registerBrowserScreenshot(r)
	registerBrowserExtract(r)
	registerBrowserBack(r)
	registerBrowserTabs(r)
}

func registerBrowserNavigate(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_navigate",
		Description: "Navigate to a URL in the browser. Returns page content + interactive elements. Requires a configured browser backend (see settings.json `browser.mode`).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"url": {"type": "string"}},
			"required": ["url"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "navigate")
		},
	})
}

func registerBrowserClick(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_click",
		Description: "Click an element by index (from elements list) or CSS selector.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"element_id": {"type": "integer"},
				"selector": {"type": "string"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "click")
		},
	})
}

func registerBrowserType(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_type",
		Description: "Type text into an input element.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"element_id": {"type": "integer"},
				"selector": {"type": "string"},
				"text": {"type": "string"}
			},
			"required": ["text"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "type")
		},
	})
}

func registerBrowserScroll(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_scroll",
		Description: "Scroll the page up or down.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"direction": {"type": "string", "enum": ["up", "down"]},
				"amount": {"type": "integer"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "scroll")
		},
	})
}

func registerBrowserScreenshot(r *Registry) {
	r.Register(&ToolSpec{
		Name:            "browser_screenshot",
		Description:     "Take a screenshot of the current page. Returns base64 PNG.",
		InputSchema:     json.RawMessage(`{"type": "object", "properties": {}}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "screenshot")
		},
	})
}

func registerBrowserExtract(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_extract",
		Description: "Extract content from the current page (a11y-style snapshot). Optional `goal` focuses extraction.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"goal": {"type": "string"}}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "extract")
		},
	})
}

func registerBrowserBack(r *Registry) {
	r.Register(&ToolSpec{
		Name:            "browser_back",
		Description:     "Navigate back in browser history.",
		InputSchema:     json.RawMessage(`{"type": "object", "properties": {}}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "back")
		},
	})
}

func registerBrowserTabs(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_tabs",
		Description: "Manage browser tabs: list, switch, new, close.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tab_action": {"type": "string", "enum": ["list", "switch", "new", "close"]},
				"tab_id": {"type": "integer"}
			},
			"required": ["tab_action"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "tabs")
		},
	})
}

// executeBrowserAction is the shared handler for all browser_* tools.
// Dispatches to the experimental backend (if installed); otherwise
// returns a clear "no backend" message.
func executeBrowserAction(ctx context.Context, input json.RawMessage, actionType string) (string, error) {
	var action BrowserAction
	if err := json.Unmarshal(input, &action); err != nil {
		return "", fmt.Errorf("parse browser input: %w", err)
	}
	action.Type = actionType

	if browserDispatchHook == nil {
		return "Browser tools are not available. " +
			"Set `browser.mode` to live, probe, or solo in settings.json " +
			"(experimental build only). Use WebFetch for basic URL fetching.", nil
	}

	result, err := browserDispatchHook(ctx, action)
	if err != nil {
		return "", fmt.Errorf("browser %s: %w", actionType, err)
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(out), nil
}
