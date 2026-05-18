// Package browsermcp exposes ycode's wire.Action browser automation
// family to foreign MCP-speaking clients (claude-code, opencode,
// codex, gemini-cli, …) over the composite /mcp/ endpoint.
//
// The internal LLM tool registry at internal/tools/browser.go ships
// the same tools to ycode's own runtime. This package wraps the
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
			Description: "Click an element. Provide one of: a CSS `selector`, an `element_id` from a prior browser_extract result, or a `match_text` substring (case-insensitive, falls back to the first matching button/link). The Ralph reliability layer retries with trimmed/unquoted selector and JS-evaluate variants before giving up; when match_text is set it also tries an extract-by-text + click path.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"element_id": {"type": "integer"},
					"selector":   {"type": "string"},
					"match_text": {"type": "string"},
					"scope":      {"type": "string"}
				}
			}`),
		},
		{
			Name:        "browser_type",
			Description: "Type text into an input element matched by CSS selector or element_id.",
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
			Description: "Capture a screenshot of the current page. Returns the PNG as an MCP image content block plus a text block with title/URL metadata. Foreign agents working under tight token budgets should pass `max_bytes` (≈200000 is safe) — if the inline base64 PNG exceeds the cap the backend re-encodes as JPEG at decreasing qualities and, if still over, spills to a file at `~/.agents/ycode/browser/screenshots/...` returning the absolute path in the `path` field instead of inline image data. Set `save_path` to force file output regardless of size (path may be absolute or relative to the screenshots dir).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"max_bytes": {"type": "integer"},
					"save_path": {"type": "string"}
				}
			}`),
		},
		{
			Name:        "browser_extract",
			Description: "Extract visible text + accessibility info from the current page. Pass `scope` (CSS selector) to constrain the query root and skip side nav; pass `match_text` or `goal` to filter elements by visible text / placeholder / aria-label (case-insensitive substring). `limit` (default 50) and `offset` paginate. Result includes `total` and `truncated` so the caller knows whether to ask for more.",
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
			Description: "Return Core Web Vitals + navigation timing from the current page. Scope-honest: not full Lighthouse — that needs Node + controlled lab-mode workloads. Navigate first, then call this.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_wait_for_selector",
			Description: "Wait for an element to reach a given lifecycle state. `state` is one of `visible` (default), `attached`, `detached`. `timeout_ms` defaults to 5000. Returns success when the state is reached or an error message describing the timeout. Cheap polling — prefer this over arbitrary sleeps after a click.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"selector":   {"type": "string"},
					"state":      {"type": "string", "enum": ["visible", "attached", "detached"]},
					"timeout_ms": {"type": "integer"}
				},
				"required": ["selector"]
			}`),
		},
		{
			Name:        "browser_keyboard_press",
			Description: "Dispatch a keyboard event. `key` is a DOM event key name (Enter, Tab, Escape, ArrowDown, a, etc.). `modifiers` is an optional subset of {Shift, Control, Alt, Meta}. Pass `selector` to focus first. Live mode dispatches a synthetic KeyboardEvent (some sites ignore non-trusted events); probe/solo dispatch real CDP key events.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"key":       {"type": "string"},
					"modifiers": {"type": "array", "items": {"type": "string"}},
					"selector":  {"type": "string"}
				},
				"required": ["key"]
			}`),
		},
		{
			Name:        "browser_clipboard_read",
			Description: "Read the system clipboard via the focused tab. Requires the live extension's clipboardRead permission (manifest 0.3.0). Returns the clipboard text in the `data` field. Foreign callers should prefer this over pbpaste + bash chaining.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "browser_clipboard_write",
			Description: "Write text to the system clipboard via the focused tab. Requires clipboardWrite permission.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"text": {"type": "string"}},
				"required": ["text"]
			}`),
		},
		{
			Name:        "browser_cookies_get",
			Description: "Return cookies for the current page (or filtered by `name` / `domain`). Live mode uses chrome.cookies.getAll so HttpOnly cookies are visible; probe/solo read document.cookie. **Sensitive** — calling this can pull session/auth tokens from a logged-in tab.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name":   {"type": "string"},
					"domain": {"type": "string"}
				}
			}`),
		},
		{
			Name:        "browser_storage_get",
			Description: "Read localStorage or sessionStorage from the current page. `storage` is `local` (default) or `session`. Pass `storage_key` to fetch a single entry; omit it to return the full key/value dump as JSON in `data`.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"storage":     {"type": "string", "enum": ["local", "session"]},
					"storage_key": {"type": "string"}
				}
			}`),
		},
		{
			Name:        "browser_capabilities",
			Description: "Probe the connected backend for its supported method list, version, and (live mode) the extension's chrome.* permissions. Foreign agents should call this once per session before relying on actions that may be missing in older extensions (the most common drift case is a stale chrome extension after a ycode upgrade).",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("browsermcp: no resources: %s", uri)
}

// RequiredMode rates every browser tool at DangerFullAccess by
// default. The posture matches the shell family: browser_eval runs
// arbitrary JS in the page, browser_type can complete logins or
// submit forms, and browser_navigate can reach any URL. Callers that
// want a stricter cap pass `_meta.permission: "ReadOnly"` on the
// request and the gate reduces it; observation-only callers see only
// the read tools below — the rest reject under that ceiling.
func (h *MCPHandler) RequiredMode(name string) mcp.PermissionMode {
	switch name {
	case "browser_screenshot",
		"browser_extract",
		"browser_network_list",
		"browser_console_get",
		"browser_lighthouse",
		"browser_perf_start",
		"browser_perf_stop",
		"browser_wait_for_selector",
		"browser_storage_get",
		"browser_capabilities":
		return mcp.ModeReadOnly
	case "browser_back",
		"browser_scroll",
		"browser_tabs",
		"browser_clipboard_read":
		return mcp.ModeWorkspaceWrite
	default:
		// navigate, click, type, eval, keyboard_press, clipboard_write,
		// cookies_get — any of these can cause writes or pull sensitive
		// state the user did not anticipate. Match the shell family's
		// ceiling.
		return mcp.ModeDangerFullAccess
	}
}

// toolToAction maps MCP tool names to the wire.Action discriminator.
var toolToAction = map[string]string{
	"browser_navigate":          wire.ActionNavigate,
	"browser_click":             wire.ActionClick,
	"browser_type":              wire.ActionType,
	"browser_scroll":            wire.ActionScroll,
	"browser_screenshot":        wire.ActionScreenshot,
	"browser_extract":           wire.ActionExtract,
	"browser_back":              wire.ActionBack,
	"browser_tabs":              wire.ActionTabs,
	"browser_eval":              wire.ActionEvaluate,
	"browser_perf_start":        wire.ActionPerfStart,
	"browser_perf_stop":         wire.ActionPerfStop,
	"browser_network_list":      wire.ActionNetworkList,
	"browser_console_get":       wire.ActionConsoleGet,
	"browser_lighthouse":        wire.ActionLighthouse,
	"browser_wait_for_selector": wire.ActionWaitForSelector,
	"browser_keyboard_press":    wire.ActionKeyboardPress,
	"browser_clipboard_read":    wire.ActionClipboardRead,
	"browser_clipboard_write":   wire.ActionClipboardWrite,
	"browser_cookies_get":       wire.ActionCookiesGet,
	"browser_storage_get":       wire.ActionStorageGet,
	"browser_capabilities":      wire.ActionCapabilities,
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
// metadata. When the screenshot was spilled to disk (Path set), the
// image block is replaced with a text block citing the path. Every
// other tool falls back to a single text block — identical to the
// legacy HandleToolCall path.
func (h *MCPHandler) HandleToolCallRich(ctx context.Context, name string, input json.RawMessage) ([]mcp.Content, error) {
	if h.client == nil {
		return []mcp.Content{mcp.ContentText(unconfiguredMessage)}, nil
	}
	result, err := h.execute(ctx, name, input)
	if err != nil {
		return nil, err
	}
	if name == "browser_screenshot" && result != nil {
		if result.Path != "" {
			meta := fmt.Sprintf("screenshot saved to %s", result.Path)
			if extra := screenshotMetadata(result); extra != "" {
				meta = extra + " — " + meta
			}
			return []mcp.Content{mcp.ContentText(meta)}, nil
		}
		if result.Image != "" {
			blocks := []mcp.Content{mcp.ContentImage(result.Image, "image/png")}
			if meta := screenshotMetadata(result); meta != "" {
				blocks = append(blocks, mcp.ContentText(meta))
			}
			return blocks, nil
		}
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
