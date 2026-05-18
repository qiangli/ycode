package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/pkg/browser/wire"
)

// RegisterBrowserHandlers registers the browser automation tools.
// All browser_* tools share one dispatch path; mode-specific actions
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
	registerBrowserEval(r)
	registerBrowserWaitForSelector(r)
	registerBrowserKeyboardPress(r)
	registerBrowserClipboardRead(r)
	registerBrowserClipboardWrite(r)
	registerBrowserCookiesGet(r)
	registerBrowserStorageGet(r)
	registerBrowserCapabilities(r)
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
		Description: "Click an element. Provide one of: CSS `selector`, `element_id` from a prior browser_extract, or `match_text` (case-insensitive substring against visible text / placeholder / aria-label). Ralph reliability layer retries with trimmed/unquoted/JS-evaluate variants; with match_text it also tries an extract-by-text + click path.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"element_id": {"type": "integer"},
				"selector":   {"type": "string"},
				"match_text": {"type": "string"},
				"scope":      {"type": "string"}
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
		Name:        "browser_screenshot",
		Description: "Take a screenshot of the current page. Returns base64 PNG. Pass `max_bytes` (≈200000 is safe) to cap inline size — over the cap the backend re-encodes as JPEG or spills to ~/.agents/ycode/browser/screenshots/ and returns `path`. Set `save_path` to force file output.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"max_bytes": {"type": "integer"},
				"save_path": {"type": "string"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionScreenshot)
		},
	})
}

func registerBrowserExtract(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_extract",
		Description: "Extract content from the current page (a11y-style snapshot). Pass `scope` to constrain the query root (skips nav by default), `match_text` or `goal` to filter elements by text, and `limit`/`offset` to paginate. Result `total`/`truncated` describe the full match count.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"scope":      {"type": "string"},
				"match_text": {"type": "string"},
				"goal":       {"type": "string"},
				"limit":      {"type": "integer"},
				"offset":     {"type": "integer"}
			}
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

func registerBrowserEval(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_eval",
		Description: "Evaluate JavaScript in the current page context and return the result as JSON. Supported by live, probe, and solo modes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"script": {"type": "string"}},
			"required": ["script"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionEvaluate)
		},
	})
}

func registerBrowserWaitForSelector(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_wait_for_selector",
		Description: "Wait for an element to reach a lifecycle state (visible|attached|detached). Prefer over arbitrary sleeps.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector":   {"type": "string"},
				"state":      {"type": "string", "enum": ["visible", "attached", "detached"]},
				"timeout_ms": {"type": "integer"}
			},
			"required": ["selector"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionWaitForSelector)
		},
	})
}

func registerBrowserKeyboardPress(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_keyboard_press",
		Description: "Dispatch a keyboard event (Enter, Tab, Escape, ArrowDown, a, …). Pass `modifiers` (Shift|Control|Alt|Meta) and optional `selector` to focus first.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"key":       {"type": "string"},
				"modifiers": {"type": "array", "items": {"type": "string"}},
				"selector":  {"type": "string"}
			},
			"required": ["key"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionKeyboardPress)
		},
	})
}

func registerBrowserClipboardRead(r *Registry) {
	r.Register(&ToolSpec{
		Name:            "browser_clipboard_read",
		Description:     "Read the system clipboard via the focused tab. Requires clipboardRead permission (live mode).",
		InputSchema:     json.RawMessage(`{"type": "object", "properties": {}}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionClipboardRead)
		},
	})
}

func registerBrowserClipboardWrite(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_clipboard_write",
		Description: "Write text to the system clipboard.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"text": {"type": "string"}},
			"required": ["text"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionClipboardWrite)
		},
	})
}

func registerBrowserCookiesGet(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_cookies_get",
		Description: "Read cookies for the current page. Sensitive — can return auth/session tokens.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name":   {"type": "string"},
				"domain": {"type": "string"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionCookiesGet)
		},
	})
}

func registerBrowserStorageGet(r *Registry) {
	r.Register(&ToolSpec{
		Name:        "browser_storage_get",
		Description: "Read localStorage or sessionStorage. `storage` is local|session; `storage_key` is optional.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"storage":     {"type": "string", "enum": ["local", "session"]},
				"storage_key": {"type": "string"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionStorageGet)
		},
	})
}

func registerBrowserCapabilities(r *Registry) {
	r.Register(&ToolSpec{
		Name:            "browser_capabilities",
		Description:     "Probe the connected backend for its supported method list, version, and (live mode) permissions. Call once per session before relying on actions that may be missing in older extensions.",
		InputSchema:     json.RawMessage(`{"type": "object", "properties": {}}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return executeBrowserAction(ctx, input, wire.ActionCapabilities)
		},
	})
}

// executeBrowserAction is the shared handler for all browser_* tools.
// Dispatches via the Client installed on ctx by main.go after
// setupBrowserBackend; otherwise returns a clear "no backend" message.
func executeBrowserAction(ctx context.Context, input json.RawMessage, actionType string) (string, error) {
	var action wire.Action
	if len(input) > 0 {
		if err := json.Unmarshal(input, &action); err != nil {
			return "", fmt.Errorf("parse browser input: %w", err)
		}
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
