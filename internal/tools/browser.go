package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/pkg/browser/wire"
)

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
			return executeBrowserAction(ctx, input, wire.ActionNavigate)
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
			return executeBrowserAction(ctx, input, wire.ActionClick)
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
			return executeBrowserAction(ctx, input, wire.ActionType)
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
			return executeBrowserAction(ctx, input, wire.ActionScroll)
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
			return executeBrowserAction(ctx, input, wire.ActionScreenshot)
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
			return executeBrowserAction(ctx, input, wire.ActionExtract)
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
			return executeBrowserAction(ctx, input, wire.ActionBack)
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
			return executeBrowserAction(ctx, input, wire.ActionTabs)
		},
	})
}

// executeBrowserAction is the shared handler for all browser_* tools.
// Dispatches via the Client installed on ctx by main.go after
// setupBrowserBackend; otherwise returns a clear "no backend" message.
func executeBrowserAction(ctx context.Context, input json.RawMessage, actionType string) (string, error) {
	var action wire.Action
	if err := json.Unmarshal(input, &action); err != nil {
		return "", fmt.Errorf("parse browser input: %w", err)
	}
	action.Type = actionType

	client, ok := browser.ClientFromContext(ctx)
	if !ok {
		return "Browser tools are not available. " +
			"Set `browser.mode` to live, probe, or solo in settings.json " +
			"(experimental build only). Use WebFetch for basic URL fetching.", nil
	}

	result, err := client.Execute(ctx, action)
	if err != nil {
		return "", fmt.Errorf("browser %s: %w", actionType, err)
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(out), nil
}
