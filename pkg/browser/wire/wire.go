// Package wire is the build-tag-free transport schema for browser
// automation. Both the tool shims (stable build) and the backend
// services (experimental build) share these types — no duplication,
// no hand-copy.
package wire

// Action is the unified browser action. Each backend translates it
// into the right native primitive (CDP command for probe/solo,
// WebSocket message for live).
type Action struct {
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

	// Extract: optional CSS scope to constrain the query root, and a
	// MatchText to filter elements by visible text. Goal is the
	// natural-language form already in use; MatchText is the
	// machine-friendly exact substring used by the click-by-text
	// strategy. Limit/Offset control pagination (default 50/0).
	Scope     string `json:"scope,omitempty"`
	MatchText string `json:"match_text,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`

	// Screenshot: opt-in size cap and save-to-file path. When MaxBytes
	// > 0 the backend tries to shrink the inline base64 PNG (JPEG
	// re-encode at decreasing qualities); if it cannot fit or
	// SavePath is set, the image is written to disk and the path is
	// returned in Result.Path instead of Result.Image.
	MaxBytes int    `json:"max_bytes,omitempty"`
	SavePath string `json:"save_path,omitempty"`

	// Wait/timeout. TimeoutMs applies to wait_for_selector (default
	// 5000ms) and is honoured by future polled actions. State is one
	// of "visible", "attached", "detached" (default "visible").
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	State     string `json:"state,omitempty"`

	// Keyboard: Key is a DOM-event key name ("Enter", "Tab", "Escape",
	// "a"), Modifiers is any subset of {"Shift","Control","Alt","Meta"}.
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`

	// Cookies. Name + Domain filter the returned set (both optional).
	Name   string `json:"name,omitempty"`
	Domain string `json:"domain,omitempty"`

	// Storage: kind is "local" or "session"; StorageKey filters to a
	// single entry, empty returns the full key/value dump.
	Storage    string `json:"storage,omitempty"`
	StorageKey string `json:"storage_key,omitempty"`
}

// Result is the unified browser result.
type Result struct {
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

	// Path is set instead of Image when the screenshot exceeded
	// MaxBytes (or SavePath was requested) and the backend wrote it to
	// disk. Always absolute.
	Path string `json:"path,omitempty"`

	// Total + Truncated describe a paginated extract: Total is the
	// element count before the Limit cap, Truncated is true when the
	// returned set is a prefix.
	Total     int  `json:"total,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// Action types recognized across modes. Backends that do not
// implement a given action return a clear "unsupported" error.
const (
	ActionNavigate   = "navigate"
	ActionClick      = "click"
	ActionType       = "type"
	ActionScroll     = "scroll"
	ActionScreenshot = "screenshot"
	ActionExtract    = "extract"
	ActionBack       = "back"
	ActionTabs       = "tabs"

	// DevTools-flavored. Evaluate is supported in all three modes
	// (live uses chrome.scripting.executeScript with world:"MAIN");
	// the perf/network/console/lighthouse actions require CDP and
	// stay probe+solo-only.
	ActionEvaluate    = "evaluate"
	ActionPerfStart   = "perf_start"
	ActionPerfStop    = "perf_stop"
	ActionNetworkList = "network_list"
	ActionConsoleGet  = "console_get"
	ActionLighthouse  = "lighthouse"

	// Robustness additions. WaitForSelector and KeyboardPress are
	// no-permission primitives that work on every mode. Clipboard /
	// Cookies / Storage require chrome.* permissions in the live
	// extension (manifest 0.3.0); probe+solo reach them via
	// CDP/evaluate. Capabilities is a session-startup probe so
	// foreign agents can discover what the connected extension
	// supports instead of guessing.
	ActionWaitForSelector = "wait_for_selector"
	ActionKeyboardPress   = "keyboard_press"
	ActionClipboardRead   = "clipboard_read"
	ActionClipboardWrite  = "clipboard_write"
	ActionCookiesGet      = "cookies_get"
	ActionStorageGet      = "storage_get"
	ActionCapabilities    = "capabilities"
)
