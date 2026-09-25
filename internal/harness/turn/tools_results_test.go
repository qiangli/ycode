package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/qiangli/bashy/pkg/harnessrunner"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
)

// forEach collects each iteration's result value directly: a denied map and
// a typed runner result must both become tool-result messages linked to
// their call ids, never a crash or a "null" payload.
func TestAppendToolResultsAcceptsCollectedResults(t *testing.T) {
	r := &Runtime{}
	typed, err := toolResultObject(harnessrunner.Result{})
	if err != nil {
		t.Fatal(err)
	}
	typed["call_id"] = "call-ok"
	in := pipeline.Invocation{Inputs: map[string]any{
		"state": map[string]any{"messages": []message.Message{}},
		"results": []any{
			map[string]any{"call_id": "call-denied", "outcome": "denied"},
			typed,
			map[string]any{"result": map[string]any{"call_id": "call-wrapped", "outcome": "rejected"}},
		},
	}}
	out := r.appendToolResults(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("appendToolResults: %v", out.Err)
	}
	state := out.Outputs["state"].(map[string]any)
	messages := state["messages"].([]message.Message)
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(messages))
	}
	for i, want := range []string{"call-denied", "call-ok", "call-wrapped"} {
		block := messages[i].Content[0]
		if block.ToolUseID != want {
			t.Errorf("message %d tool id = %q, want %q", i, block.ToolUseID, want)
		}
		if block.Content == "null" || !strings.Contains(block.Content, want) {
			t.Errorf("message %d content = %q", i, block.Content)
		}
	}
}
