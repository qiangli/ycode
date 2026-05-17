package live

import (
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// TestActionToParams_Evaluate pins the routing for the evaluate
// action: live mode now supports it, so the dispatcher must produce a
// {"evaluate", {"script": "..."}} pair — not the legacy
// "action not supported" error that drove agents to write
// ad-hoc Python decode dances when they wanted to inspect page state.
func TestActionToParams_Evaluate(t *testing.T) {
	method, params, err := actionToParams(mcpservers.BrowserAction{
		Type:   mcpservers.ActionEvaluate,
		Script: "document.title",
	})
	if err != nil {
		t.Fatalf("actionToParams(evaluate): unexpected err: %v", err)
	}
	if method != "evaluate" {
		t.Fatalf("method = %q; want evaluate", method)
	}
	if got, _ := params["script"].(string); got != "document.title" {
		t.Fatalf("params.script = %v; want document.title", params["script"])
	}
}

func TestActionToParams_UnknownStillErrors(t *testing.T) {
	if _, _, err := actionToParams(mcpservers.BrowserAction{Type: "does-not-exist"}); err == nil {
		t.Fatal("expected error for unknown action; got nil")
	}
}
