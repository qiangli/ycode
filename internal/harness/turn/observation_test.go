package turn

// Sprint: #290; Story: #883; Story-ID: f8779a727bcb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

func chunk(s string) []any {
	return []any{map[string]any{"data": base64.StdEncoding.EncodeToString([]byte(s)), "encoding": "base64", "from": float64(0), "to": float64(len(s))}}
}

func TestObservationTextReadsLikeATerminal(t *testing.T) {
	for name, tc := range map[string]struct {
		fields map[string]any
		want   string
	}{
		"stdout": {map[string]any{"outcome": "completed", "process": map[string]any{"exitCode": float64(0)},
			"output": map[string]any{"stdout": chunk("./stats.py\n"), "truncated": false}}, "exit 0\n./stats.py"},
		"stderr and exit code": {map[string]any{"outcome": "completed", "process": map[string]any{"exitCode": float64(2)},
			"output": map[string]any{"stderr": chunk("grep: x: No such file or directory\n")}}, "exit 2\nstderr:\ngrep: x: No such file or directory"},
		"truncated": {map[string]any{"outcome": "completed", "process": map[string]any{"exitCode": float64(0)},
			"output": map[string]any{"stdout": chunk("a\n"), "truncated": true}}, "exit 0\na\n[output truncated]"},
		"timed out":              {map[string]any{"outcome": "timed_out", "output": map[string]any{}}, "timed_out"},
		"denied with rule":       {map[string]any{"call_id": "c", "outcome": "denied", "rule_id": "not-checkable"}, `denied by policy rule "not-checkable"; the command did not run`},
		"denied, does not parse": {map[string]any{"call_id": "c", "outcome": "denied", "rule_id": "not-checkable", "unsupported": []any{map[string]any{"kind": "syntax", "value": "foo(", "reason": "the command does not parse: <harness>:1:1: `foo(` must be followed by `)`"}}}, "denied by policy rule \"not-checkable\"; the command did not run: the command does not parse: <harness>:1:1: `foo(` must be followed by `)`"},
		"denied":                 {map[string]any{"call_id": "c", "outcome": "denied"}, "denied by policy; the command did not run"},
		"rejected":               {map[string]any{"call_id": "c", "outcome": "rejected"}, "rejected by the reviewer; the command did not run"},
	} {
		if got := observationText(tc.fields); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// with.format observation changes only what the model reads; without it the
// message is the full record, as before (the agent-mini baseline).
func TestAppendToolResultsObservationFormat(t *testing.T) {
	r := &Runtime{}
	result := map[string]any{"call_id": "call-1", "outcome": "completed", "process": map[string]any{"exitCode": float64(0)},
		"output": map[string]any{"stdout": chunk("hello\n")}}
	run := func(with map[string]any) string {
		out := r.appendToolResults(context.Background(), pipeline.Invocation{With: with, Inputs: map[string]any{
			"state": map[string]any{"messages": []message.Message{}}, "results": []any{result}}})
		if out.Err != nil {
			t.Fatal(out.Err)
		}
		block := out.Outputs["state"].(map[string]any)["messages"].([]message.Message)[0].Content[0]
		if block.ToolUseID != "call-1" {
			t.Fatalf("tool id %q", block.ToolUseID)
		}
		return block.Content
	}
	if got := run(map[string]any{"format": "observation"}); got != "exit 0\nhello" {
		t.Fatalf("observation = %q", got)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(run(nil)), &record); err != nil || record["call_id"] != "call-1" {
		t.Fatalf("default must stay the JSON record, got %v", err)
	}
}

func TestDenyReasonIsOptIn(t *testing.T) {
	r := &Runtime{}
	intent := hitl.Call{ID: "c1", Name: "bashy", Script: "python3 x.py"}
	decision := hitl.Decision{PolicyRef: "workspace", RuleID: "not-checkable", Decision: "deny"}
	for _, reason := range []bool{false, true} {
		out := r.deny(context.Background(), pipeline.Invocation{With: map[string]any{"reason": reason}, Inputs: map[string]any{"intent": intent, "decision": decision}})
		if out.Err != nil {
			t.Fatalf("deny: %v", out.Err)
		}
		res := out.Outputs["result"].(map[string]any)
		if _, has := res["rule_id"]; has != reason {
			t.Fatalf("reason=%v: rule_id present=%v (%v)", reason, has, res)
		}
	}
}
