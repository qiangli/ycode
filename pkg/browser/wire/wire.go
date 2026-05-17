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
)
