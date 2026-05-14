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

import "github.com/qiangli/ycode/pkg/browser/wire"

// BrowserAction is the unified action format. Aliased from
// pkg/browser/wire so the tool shim (stable build) and the backends
// (experimental build) share one definition.
type BrowserAction = wire.Action

// BrowserResult is the unified result. The reliability layer adds
// Hints and OutcomeClass; raw backends leave them empty.
type BrowserResult = wire.Result

// Action types recognized across modes. Backends that do not
// implement a given action return a clear "unsupported" error.
const (
	ActionNavigate   = wire.ActionNavigate
	ActionClick      = wire.ActionClick
	ActionType       = wire.ActionType
	ActionScroll     = wire.ActionScroll
	ActionScreenshot = wire.ActionScreenshot
	ActionExtract    = wire.ActionExtract
	ActionBack       = wire.ActionBack
	ActionTabs       = wire.ActionTabs

	// DevTools-flavored (probe + solo support; live returns
	// unsupported).
	ActionEvaluate    = wire.ActionEvaluate
	ActionPerfStart   = wire.ActionPerfStart
	ActionPerfStop    = wire.ActionPerfStop
	ActionNetworkList = wire.ActionNetworkList
	ActionConsoleGet  = wire.ActionConsoleGet
	ActionLighthouse  = wire.ActionLighthouse
)
