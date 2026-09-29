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

// Sprint 322 (django__django-15280): tool observations entered the
// transcript unbounded, so one large sed/test-log dump stayed in every later
// request and in the committed session. With YAML maxChars the model sees
// the head and tail plus a note of what was left out.
func TestAppendToolResultsBoundsObservationsByMaxChars(t *testing.T) {
	r := &Runtime{}
	stdout := "HEAD\n" + strings.Repeat("0123456789012345678901234567890\n", 2000) + "TAIL\n"
	result := map[string]any{"call_id": "big", "outcome": "completed",
		"process": map[string]any{"exitCode": 0},
		"output":  map[string]any{"stdout": []any{map[string]any{"data": stdout, "encoding": "utf-8", "from": 0}}}}
	in := pipeline.Invocation{
		With:   map[string]any{"format": "observation", "maxChars": 10000},
		Inputs: map[string]any{"state": map[string]any{"messages": []message.Message{}}, "results": []any{result}},
	}
	out := r.appendToolResults(context.Background(), in)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	content := out.Outputs["state"].(map[string]any)["messages"].([]message.Message)[0].Content[0].Content
	if len(content) > 10200 {
		t.Fatalf("observation = %d bytes, want about 10000", len(content))
	}
	if !strings.HasPrefix(content, "exit 0\nHEAD") || !strings.HasSuffix(content, "TAIL") || !strings.Contains(content, "[output truncated: ") {
		t.Fatalf("observation must keep head, tail and a truncation note: %.60q ... %.60q", content, content[len(content)-60:])
	}
	in.With["maxChars"] = 0
	if out := r.appendToolResults(context.Background(), in); out.Err == nil {
		t.Fatal("maxChars 0 must be rejected, not silently unbounded")
	}
}
