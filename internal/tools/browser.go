package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/runtime/browseruse"
)

// browserService is the shared browser container service.
var browserService *browseruse.Service

// SetBrowserService sets the browser-use service for browser tools.
func SetBrowserService(svc *browseruse.Service) {
	browserService = svc
}

// InitBrowserService creates and starts a browser-use service using the container engine.
func InitBrowserService(engine *container.Engine, sessionID string, network string, allowedDomains []string) *browseruse.Service {
	svc := browseruse.NewService(engine, sessionID, network, allowedDomains)
	browserService = svc
	return svc
}

// RegisterBrowserHandlers registers the browser automation tools.
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
		Description: "Navigate to a URL in the browser. Returns page content and interactive elements list. Requires container engine.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "URL to navigate to"}
			},
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
		Description: "Click an element in the browser by element index (from elements list) or CSS selector.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"element_id": {"type": "integer", "description": "Element index from the elements list (e.g., 1, 2, 3)"},
				"selector": {"type": "string", "description": "CSS selector (alternative to element_id)"}
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
		Description: "Type text into an input element in the browser.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"element_id": {"type": "integer", "description": "Input element index from elements list"},
				"selector": {"type": "string", "description": "CSS selector (alternative to element_id)"},
				"text": {"type": "string", "description": "Text to type"}
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
		Description: "Scroll the browser page up or down.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"direction": {"type": "string", "enum": ["up", "down"], "description": "Scroll direction (default: down)"},
				"amount": {"type": "integer", "description": "Scroll amount in pixels (default: 500)"}
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
		Name:        "browser_screenshot",
		Description: "Take a screenshot of the current browser page. Returns base64-encoded PNG.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "screenshot")
		},
	})
}

func registerBrowserExtract(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_extract",
		Description: "Extract content from the current page. Optionally specify a goal to focus extraction.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"goal": {"type": "string", "description": "What to extract from the page (natural language description)"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "extract")
		},
	})
}

func registerBrowserBack(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_back",
		Description: "Navigate back to the previous page in browser history.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "back")
		},
	})
}

func registerBrowserTabs(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_tabs",
		Description: "Manage browser tabs: list, switch, open new, or close current tab.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tab_action": {"type": "string", "enum": ["list", "switch", "new", "close"], "description": "Tab action to perform"},
				"tab_id": {"type": "integer", "description": "Tab index to switch to (for switch action)"}
			},
			"required": ["tab_action"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, "tabs")
		},
	})
}

// executeBrowserAction is the shared handler for all browser tools.
// The browser container is started lazily on first use (the image is large).
func executeBrowserAction(ctx context.Context, input json.RawMessage, actionType string) (string, error) {
	if browserService == nil {
		return "Browser tools are not available (container engine not initialized). " +
			"Use WebFetch for basic URL fetching instead.", nil
	}

	// Lazy start: build image and start container on first browser tool call.
	if !browserService.Available() {
		if err := browserService.Start(ctx); err != nil {
			return "", fmt.Errorf("browser: failed to start container: %w", err)
		}
	}

	var action browseruse.Action
	if err := json.Unmarshal(input, &action); err != nil {
		return "", fmt.Errorf("parse browser input: %w", err)
	}
	action.Type = actionType

	result, err := browserService.Execute(ctx, action)
	if err != nil {
		return "", fmt.Errorf("browser %s: %w", actionType, err)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(out), nil
}
