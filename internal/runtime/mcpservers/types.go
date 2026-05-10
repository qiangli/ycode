//go:build experimental

// Package mcpservers provides ycode's pure-Go browser automation
// stack. Three modes share one BrowserAction surface:
//
//	live  — ycode-owned MV3 extension; drives the user's real Chrome
//	probe — chromedp attaches to a Chrome started with debug port
//	solo  — chromedp launches a fresh isolated Chrome
//
// A shared reliability layer (Hint Engine, Ralph fallback, circuit
// breaker, DOM compression, Pattern Learner, Outcome Classifier),
// inspired by openchrome (MIT), wraps every Service so the primitives
// apply uniformly. See internal/runtime/mcpservers/reliability.
package mcpservers

// BrowserAction is the unified action format. Each backend
// translates it into the right native primitive (CDP command for
// probe/solo, WebSocket message for live).
type BrowserAction struct {
	Type      string `json:"action"`
	URL       string `json:"url,omitempty"`
	ElementID int    `json:"element_id,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Direction string `json:"direction,omitempty"`
	Amount    int    `json:"amount,omitempty"`
	Goal      string `json:"goal,omitempty"`
	TabID     int    `json:"tab_id,omitempty"`
	TabAction string `json:"tab_action,omitempty"`

	// DevTools-specific extensions (probe + solo only).
	Script string `json:"script,omitempty"` // for evaluate / lighthouse opts
	URLs   []string `json:"urls,omitempty"` // for network_list filters
}

// Action types recognized across modes. Backends that do not
// implement a given action return a clear "unsupported" error.
const (
	ActionNavigate    = "navigate"
	ActionClick       = "click"
	ActionType        = "type"
	ActionScroll      = "scroll"
	ActionScreenshot  = "screenshot"
	ActionExtract     = "extract"
	ActionBack        = "back"
	ActionTabs        = "tabs"

	// DevTools-flavored (probe + solo support; live returns
	// unsupported).
	ActionEvaluate    = "evaluate"
	ActionPerfStart   = "perf_start"
	ActionPerfStop    = "perf_stop"
	ActionNetworkList = "network_list"
	ActionConsoleGet  = "console_get"
	ActionLighthouse  = "lighthouse"
)

// BrowserResult is the unified result. The reliability layer adds
// Hints and OutcomeClass; raw backends leave them empty.
type BrowserResult struct {
	Success  bool   `json:"success"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Content  string `json:"content,omitempty"`
	Elements string `json:"elements,omitempty"`
	Data     string `json:"data,omitempty"`
	Image    string `json:"image,omitempty"` // base64 PNG
	Error    string `json:"error,omitempty"`

	// Added by reliability.Wrap.
	Hints        []string `json:"hints,omitempty"`         // Hint Engine annotations
	OutcomeClass string   `json:"outcome_class,omitempty"` // SUCCESS | SILENT_CLICK | WRONG_ELEMENT | AUTH_REDIRECT | BLOCKED
}
