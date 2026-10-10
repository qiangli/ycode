package turn

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/harness/pipeline"
)

// Sprint 412 story #1819 (django-14787): the model answered with TEXT bodies
// holding tool calls (a well-formed {"tool_calls":[{"name":"bashy",...}]}
// object where structured tool calls should have been) and the harness
// treated them as plain text. The turn finished with no diff. Text that is
// exactly such an object must normalize into real tool calls.

// textCallSample mirrors the live failure: the opus reply body carried the
// bashy call as JSON text, backticks already replaced as in the report.
const textCallSample = `{"tool_calls":[{"name":"bashy","arguments":{"script":"@effects(\"read,write,exec\")\n@contain(net: \"deny\")\nfunction apply_patch() {\n  python - <<'EOF'\nprint('patched')\nEOF\n}\napply_patch && git diff"}}]}`

const textCallScript = `@effects("read,write,exec")` + "\n" + `@contain(net: "deny")` + "\n" + `function apply_patch() {
  python - <<'EOF'
print('patched')
EOF
}
apply_patch && git diff`

func TestRecoverTextToolCallsWholeObject(t *testing.T) {
	calls, ok := recoverTextToolCalls(textCallSample)
	if !ok {
		t.Fatalf("recoverTextToolCalls refused the sample text body")
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one recovered call", calls)
	}
	call, ok := calls[0].(map[string]any)
	if !ok || call["name"] != "bashy" {
		t.Fatalf("call = %#v, want the bashy call", calls[0])
	}
	input, ok := call["input"].(map[string]any)
	if !ok || input["script"] != textCallScript {
		t.Fatalf("input = %#v, want the sample script", call["input"])
	}
	if id, _ := call["id"].(string); id == "" {
		t.Fatal("recovered call has no id")
	}
}

func TestRecoverTextToolCallsShapes(t *testing.T) {
	cases := map[string]string{
		"single call object": `{"name":"bashy","arguments":{"script":"printf hi"}}`,
		"camelCase key":      `{"toolCalls":[{"name":"bashy","arguments":{"script":"printf hi"}}]}`,
		"fenced block":       "Here is the call:\n```json\n" + `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi"}}]}` + "\n```\nLet me know how it goes.",
		"fenced no tag":      "```\n" + `{"name":"bashy","arguments":{"script":"printf hi"}}` + "\n```",
	}
	for name, body := range cases {
		calls, ok := recoverTextToolCalls(body)
		if !ok || len(calls) != 1 {
			t.Fatalf("%s: calls = %#v, ok = %v, want one recovered call", name, calls, ok)
		}
	}
}

func TestRecoverTextToolCallsRefusals(t *testing.T) {
	cases := map[string]string{
		// Prose that merely mentions JSON must not trigger recovery.
		"prose": `To call the tool, reply with {"tool_calls": [...]} format and include your script.`,
		// Truncated JSON must not trigger recovery.
		"partial": `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi`,
		// A tool that does not exist must not trigger recovery.
		"unknown tool": `{"tool_calls":[{"name":"shell","arguments":{"script":"printf hi"}}]}`,
		// No script, nothing to execute.
		"missing script": `{"tool_calls":[{"name":"bashy","arguments":{}}]}`,
		"empty script":   `{"name":"bashy","arguments":{"script":"   "}}`,
		"empty array":    `{"tool_calls":[]}`,
		// More than one fenced block is ambiguous, never a single call.
		"two fences": "```json\n" + `{"name":"bashy","arguments":{"script":"one"}}` + "\n```\n```json\n" + `{"name":"bashy","arguments":{"script":"two"}}` + "\n```",
		// Not an object at all.
		"array":     `[{"name":"bashy","arguments":{"script":"printf hi"}}]`,
		"unrelated": `{"status":"done","summary":"patched the file"}`,
		"empty":     ``,
	}
	for name, body := range cases {
		if calls, ok := recoverTextToolCalls(body); ok {
			t.Fatalf("%s: recovered %#v, want refusal", name, calls)
		}
	}
}

// A text reply with no structured calls must leave normalize carrying real
// tool calls: the dispatch switch routes on hasToolCalls, so the recovered
// response must set it (and clear finished, like a real tool-call turn).
func TestNormalizeRecoversTextToolCalls(t *testing.T) {
	r := &Runtime{}
	response := map[string]any{"text": textCallSample, "toolCalls": []any{}, "hasToolCalls": false, "finished": true}
	in := pipeline.Invocation{
		StageID: "test",
		Inputs:  map[string]any{"state": map[string]any{}, "response": response},
		With:    map[string]any{"deterministicCallIds": true, "duplicateCalls": "drop-identical", "malformedToolResult": "repair-explicitly"},
	}
	out := r.normalize(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("normalize: %v", out.Err)
	}
	recovered := out.Outputs["state"].(map[string]any)["response"].(map[string]any)
	calls, _ := recovered["toolCalls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("toolCalls = %#v, want the recovered bashy call", recovered["toolCalls"])
	}
	if recovered["hasToolCalls"] != true {
		t.Fatalf("hasToolCalls = %#v, want true so dispatch executes", recovered["hasToolCalls"])
	}
	if recovered["finished"] != false {
		t.Fatalf("finished = %#v, want false like a real tool-call turn", recovered["finished"])
	}
	call := calls[0].(map[string]any)
	if call["name"] != "bashy" || call["input"].(map[string]any)["script"] != textCallScript {
		t.Fatalf("call = %#v, want the sample bashy call", call)
	}
}

// Structured calls win: text that also mentions JSON must not add calls.
func TestNormalizeKeepsStructuredCallsOverText(t *testing.T) {
	r := &Runtime{}
	structured := map[string]any{"id": "call-1", "name": "bashy", "input": map[string]any{"script": "printf real"}}
	response := map[string]any{"text": textCallSample, "toolCalls": []any{structured}, "hasToolCalls": true, "finished": false}
	in := pipeline.Invocation{
		Inputs: map[string]any{"state": map[string]any{}, "response": response},
	}
	out := r.normalize(context.Background(), in)
	if out.Err != nil {
		t.Fatalf("normalize: %v", out.Err)
	}
	calls := out.Outputs["state"].(map[string]any)["response"].(map[string]any)["toolCalls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["id"] != "call-1" {
		t.Fatalf("toolCalls = %#v, want the structured call untouched", calls)
	}
}
