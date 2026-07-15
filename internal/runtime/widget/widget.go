// Package widget hosts the canvas-side event hooks (alerts, health) that
// publish structured display payloads onto the bus. The generative-UI
// surface is driven over the event channel; the MCP projection of the
// canvas has been removed (ycode is driven, not exposed).
package widget

import "encoding/json"

// DefaultSession is the well-known session ID used when a caller
// (especially a foreign agent that hasn't been handed a session by
// the user) doesn't specify one. The /canvas/ route subscribes to
// this session by default, so the trivial-case round-trip works
// without explicit session plumbing.
const DefaultSession = "canvas-default"

// iframePayload is the inner Data of an EventStateUpdate with
// format="iframe". HTML is verbatim — the canvas-side bridge wraps it
// with the bridge.js shim that supplies postMessage + ResizeObserver
// behavior.
type iframePayload struct {
	Format   string `json:"format"` // "iframe"
	WidgetID string `json:"widget_id"`
	HTML     string `json:"html"`
	Origin   string `json:"origin,omitempty"`
}

// a2uiPayload is the inner Data of an EventStateUpdate with
// format="a2ui". Wraps a batch of ops in the v0.9 OperationsKey
// container so renderers can validate-and-route by looking for that key.
type a2uiPayload struct {
	Format string          `json:"format"`           // "a2ui"
	Body   json.RawMessage `json:"body"`             // {"a2ui_operations": [...]}
	Origin string          `json:"origin,omitempty"` // publisher name, if known
}
