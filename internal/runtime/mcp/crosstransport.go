package mcp

import "fmt"

// crossTransportTools maps tool name → transport that exposes it
// ("stdio" or "http"). It exists only to make the unknown-tool error
// emitted by CompositeHandler more actionable: when a client calls a
// tool that isn't on its current transport but IS known to live on
// the other one, the error names the sibling channel and the
// `ycode pair` command to wire it in.
//
// The table is intentionally static. Both transports actually share
// the same CompositeHandler type — the divergence is which handlers
// each entrypoint (`ycode mcp serve` vs `ycode serve`) registers. A
// parity test (composite_crosshint_test.go) walks both production
// compositions and asserts every entry here points at a transport
// that really does advertise the tool, so the table can't drift
// without the test catching it.
var crossTransportTools = map[string]string{
	// Browser family — registered only by `ycode serve` (HTTP).
	"browser_navigate":          "http",
	"browser_click":             "http",
	"browser_type":              "http",
	"browser_scroll":            "http",
	"browser_screenshot":        "http",
	"browser_extract":           "http",
	"browser_back":              "http",
	"browser_tabs":              "http",
	"browser_eval":              "http",
	"browser_perf_start":        "http",
	"browser_perf_stop":         "http",
	"browser_network_list":      "http",
	"browser_console_get":       "http",
	"browser_lighthouse":        "http",
	"browser_wait_for_selector": "http",
	"browser_keyboard_press":    "http",
	"browser_clipboard_read":    "http",
	"browser_clipboard_write":   "http",
	"browser_cookies_get":       "http",
	"browser_storage_get":       "http",
	"browser_capabilities":      "http",

	// Loom (worktree coordination) — HTTP only.
	"loom_lease":   "http",
	"loom_push":    "http",
	"loom_merge":   "http",
	"loom_release": "http",
	"loom_status":  "http",

	// Observability / pulse — HTTP only (needs ycode serve running).
	"promql_query":            "http",
	"promql_query_range":      "http",
	"query_logs":              "http",
	"query_traces":            "http",
	"list_prometheus_metrics": "http",
	"search_victorialogs":     "http",
	"query_metrics":           "http",
}

// CrossTransportTools returns a copy of the cross-transport tool map
// (tool name → owning transport). Exported so the catalog-lint test
// in cmd/ycode can verify every `mcp:` entry in catalog.yaml is
// known to at least one transport. Returns a fresh map; callers may
// mutate without affecting the package-level state.
func CrossTransportTools() map[string]string {
	out := make(map[string]string, len(crossTransportTools))
	for k, v := range crossTransportTools {
		out[k] = v
	}
	return out
}

// unknownToolErr returns the unknown-tool error for this composite.
// When the missing tool is in crossTransportTools and points at a
// transport other than this composite's, the error gains a sibling-
// transport hint plus a `ycode pair` command the agent can run to
// wire it. Otherwise the error is the bare legacy form so existing
// callers parsing on the "unknown tool:" prefix still match.
func (c *CompositeHandler) unknownToolErr(name string) error {
	owner, known := crossTransportTools[name]
	if c.transport == "" || !known || owner == c.transport {
		return fmt.Errorf("unknown tool: %s", name)
	}
	wireCmd := "ycode serve"
	if owner == "stdio" {
		wireCmd = "ycode mcp serve"
	}
	return fmt.Errorf(
		"unknown tool: %s on transport %q; available on %q — start with '%s' and run 'ycode pair --tool %s' to wire it",
		name, c.transport, owner, wireCmd, name,
	)
}
