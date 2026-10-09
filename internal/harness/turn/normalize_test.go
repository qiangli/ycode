package turn

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/harness/pipeline"
)

// Sprint 379 Story #51 (a87716307f79): a headless genie worker (glm-5.3) had
// every bashy call dispatched twice per step with identical argv. The
// compiled pipeline already declares messages.normalize-provider-response
// with duplicateCalls: drop-identical and deterministicCallIds: true
// (examples/agent.yaml, examples/agent-mini/agent.yaml, examples/genie/agent.yaml)
// expecting this stage to dedupe before dispatch, but the Go handler ignored
// every `with` field and passed the response through untouched.
func TestNormalizeDropsIdenticalToolCallsWithinOneStep(t *testing.T) {
	r := &Runtime{}
	response := map[string]any{
		"toolCalls": []any{
			map[string]any{"id": "call-a", "name": "bashy", "input": map[string]any{"script": "sed -n '1,50p' chat.go"}},
			map[string]any{"id": "call-b", "name": "bashy", "input": map[string]any{"script": "sed -n '1,50p' chat.go"}},
			map[string]any{"id": "call-c", "name": "bashy", "input": map[string]any{"script": "cat other.go"}},
		},
	}
	in := pipeline.Invocation{
		Inputs: map[string]any{"state": map[string]any{"messages": []any{}}, "response": response},
		With:   map[string]any{"deterministicCallIds": true, "duplicateCalls": "drop-identical", "malformedToolResult": "repair-explicitly"},
	}
	out := r.normalize(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("normalize: %v", out.Err)
	}
	state := out.Outputs["state"].(map[string]any)
	calls := state["response"].(map[string]any)["toolCalls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("toolCalls = %#v, want the identical second call dropped", calls)
	}
	first, second := calls[0].(map[string]any), calls[1].(map[string]any)
	if first["id"] == "" || second["id"] == "" || first["id"] == second["id"] {
		t.Fatalf("deterministic ids missing or colliding: %#v / %#v", first["id"], second["id"])
	}
}

// Without deterministicCallIds, dedup still collapses by (name, input): a
// provider that echoes the same call under two different ids must not
// dispatch it twice either.
func TestNormalizeDropsIdenticalCallsByContentEvenWithoutDeterministicIds(t *testing.T) {
	r := &Runtime{}
	response := map[string]any{
		"toolCalls": []any{
			map[string]any{"id": "provider-id-1", "name": "bashy", "input": map[string]any{"script": "grep -n foo bar.go"}},
			map[string]any{"id": "provider-id-2", "name": "bashy", "input": map[string]any{"script": "grep -n foo bar.go"}},
		},
	}
	in := pipeline.Invocation{
		Inputs: map[string]any{"state": map[string]any{}, "response": response},
		With:   map[string]any{"duplicateCalls": "drop-identical"},
	}
	out := r.normalize(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("normalize: %v", out.Err)
	}
	calls := out.Outputs["state"].(map[string]any)["response"].(map[string]any)["toolCalls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("toolCalls = %#v, want the echoed duplicate dropped", calls)
	}
	if calls[0].(map[string]any)["id"] != "provider-id-1" {
		t.Fatalf("kept call = %#v, want the first occurrence preserved", calls[0])
	}
}

// A call with no input (a malformed provider echo) gets an explicit empty
// object instead of propagating nil into dispatch.
func TestNormalizeRepairsMalformedToolCallInput(t *testing.T) {
	r := &Runtime{}
	response := map[string]any{"toolCalls": []any{map[string]any{"id": "call-a", "name": "bashy", "input": nil}}}
	in := pipeline.Invocation{
		Inputs: map[string]any{"state": map[string]any{}, "response": response},
		With:   map[string]any{"malformedToolResult": "repair-explicitly"},
	}
	out := r.normalize(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("normalize: %v", out.Err)
	}
	calls := out.Outputs["state"].(map[string]any)["response"].(map[string]any)["toolCalls"].([]any)
	input, ok := calls[0].(map[string]any)["input"].(map[string]any)
	if !ok || input == nil {
		t.Fatalf("input = %#v, want an explicit empty object", calls[0].(map[string]any)["input"])
	}
}

// Without the compiled `with` policy at all, normalize must still pass the
// response through unchanged (a declared stage with no options is a no-op,
// not a failure).
func TestNormalizeWithoutPolicyIsANoOp(t *testing.T) {
	r := &Runtime{}
	response := map[string]any{"toolCalls": []any{
		map[string]any{"id": "call-a", "name": "bashy", "input": map[string]any{"script": "same"}},
		map[string]any{"id": "call-b", "name": "bashy", "input": map[string]any{"script": "same"}},
	}}
	in := pipeline.Invocation{Inputs: map[string]any{"state": map[string]any{}, "response": response}}
	out := r.normalize(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("normalize: %v", out.Err)
	}
	calls := out.Outputs["state"].(map[string]any)["response"].(map[string]any)["toolCalls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("toolCalls = %#v, want unchanged", calls)
	}
}
