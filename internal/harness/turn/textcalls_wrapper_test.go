package turn

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/provider"
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
)

// Sprint 413 story #1846 (S329 steer/t1-pivot, genie-opus5): after the steer
// the model wrote its bashy call as assistant TEXT in the tool_calls wrapper
// shape, but with the call entry never closed (`}]}` where `}}]}` belongs).
// The strict parse refused it, genie printed the JSON and the turn ended
// with no tool run.

// s329WrapperText is the journaled response text from that run, verbatim.
const s329WrapperText = `{"tool_calls":[{"name":"bashy","arguments":{"script":"@effects(\"read,write,exec\")\n@contain(net: \"deny\")\nfunction run_checked() {\ncat >> inv.py <<'EOF'\n\n\ndef report():\n    \"\"\"Return \\\"name: qty\\\" strings sorted by name.\"\"\"\n    return [f\"{name}: {qty}\" for name, qty in sorted(_items.items())]\nEOF\ntail -6 inv.py\n}\nrun_checked"}]}`

const s329WrapperScript = `@effects("read,write,exec")
@contain(net: "deny")
function run_checked() {
cat >> inv.py <<'EOF'


def report():
    """Return \"name: qty\" strings sorted by name."""
    return [f"{name}: {qty}" for name, qty in sorted(_items.items())]
EOF
tail -6 inv.py
}
run_checked`

func TestRecoverTextToolCallsS329UnclosedEntry(t *testing.T) {
	var strict map[string]any
	if err := json.Unmarshal([]byte(s329WrapperText), &strict); err == nil {
		t.Fatal("sample parses strictly; it must reproduce the unclosed-entry defect")
	}
	calls, repaired, ok := recoverTextToolCallsDetail(s329WrapperText)
	if !ok || len(calls) != 1 {
		t.Fatalf("calls = %#v, ok = %v, want the one bashy call", calls, ok)
	}
	if !repaired {
		t.Fatal("repaired = false, want the closer repair reported")
	}
	call := calls[0].(map[string]any)
	if call["name"] != provider.ToolName || call["input"].(map[string]any)["script"] != s329WrapperScript {
		t.Fatalf("call = %#v, want the S329 script", call)
	}
	fenced := "Running it now.\n```json\n" + s329WrapperText + "\n```"
	if calls, ok := recoverTextToolCalls(fenced); !ok || len(calls) != 1 {
		t.Fatalf("fenced S329 body: calls = %#v, ok = %v", calls, ok)
	}
}

func TestRecoverTextToolCallsOpenAINestedForm(t *testing.T) {
	cases := map[string]string{
		"nested object args":  `{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bashy","arguments":{"script":"printf hi"}}}]}`,
		"nested string args":  `{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bashy","arguments":"{\"script\":\"printf hi\"}"}}]}`,
		"nested no type":      `{"tool_calls":[{"function":{"name":"bashy","arguments":{"script":"printf hi"}}}]}`,
		"flat string args":    `{"tool_calls":[{"name":"bashy","arguments":"{\"script\":\"printf hi\"}"}]}`,
		"single nested":       `{"type":"function","function":{"name":"bashy","arguments":{"script":"printf hi"}}}`,
		"nested unclosed fn":  `{"tool_calls":[{"type":"function","function":{"name":"bashy","arguments":{"script":"printf hi"}}]}`,
		"missing array close": `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi"}}}`,
	}
	for name, body := range cases {
		calls, ok := recoverTextToolCalls(body)
		if !ok || len(calls) != 1 {
			t.Fatalf("%s: calls = %#v, ok = %v, want one recovered call", name, calls, ok)
		}
		input := calls[0].(map[string]any)["input"].(map[string]any)
		if input["script"] != "printf hi" {
			t.Fatalf("%s: input = %#v", name, input)
		}
	}
}

func TestRecoverTextToolCallsWrapperRefusals(t *testing.T) {
	cases := map[string]string{
		// The repair never makes an unknown tool or missing script valid.
		"repaired unknown tool":   `{"tool_calls":[{"name":"shell","arguments":{"script":"printf hi"}]}`,
		"repaired missing script": `{"tool_calls":[{"name":"bashy","arguments":{"cmd":"printf hi"}]}`,
		// Truncation is never guessed at: open containers or an open string
		// at end of input refuse.
		"truncated after script": `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi"}`,
		"truncated in string":    `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi]}`,
		// A closer that matches nothing open refuses.
		"stray closer": `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi"}}]}]}`,
		// Too far from well-formed to repair.
		"three missing closers": `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi","env":{"A":[1}`,
		// One bad entry refuses the whole body even when repaired.
		"repaired mixed entries": `{"tool_calls":[{"name":"bashy","arguments":{"script":"one"}},{"name":"rm","arguments":{"script":"two"}]}`,
		// Nested-form ambiguity and wrong kinds refuse.
		"nested and flat":       `{"tool_calls":[{"name":"bashy","arguments":{"script":"a"},"function":{"name":"bashy","arguments":{"script":"b"}}}]}`,
		"nested wrong type":     `{"tool_calls":[{"type":"custom","function":{"name":"bashy","arguments":{"script":"printf hi"}}}]}`,
		"nested unknown tool":   `{"tool_calls":[{"type":"function","function":{"name":"exec","arguments":{"script":"printf hi"}}}]}`,
		"string args not json":  `{"tool_calls":[{"name":"bashy","arguments":"printf hi"}]}`,
		"string args not obj":   `{"tool_calls":[{"name":"bashy","arguments":"[\"printf hi\"]"}]}`,
		"string args no script": `{"tool_calls":[{"name":"bashy","arguments":"{\"cmd\":\"printf hi\"}"}]}`,
		// Prose around an unfenced object, or prose with a mismatched brace.
		"prose prefix": `I will run: {"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi"}]}`,
		"prose braces": `The array [a, {b] is wrong.`,
	}
	for name, body := range cases {
		if calls, ok := recoverTextToolCalls(body); ok {
			t.Fatalf("%s: recovered %#v, want refusal", name, calls)
		}
	}
}

func TestRepairMismatchedClosersWellFormedIsNoRepair(t *testing.T) {
	if _, changed := repairMismatchedClosers(textCallSample); changed {
		t.Fatal("a well-formed document needs no repair")
	}
	// Braces and brackets inside strings, including escaped quotes, are
	// not structure.
	in := `{"a":[{"s":"x}]\"]}"]}`
	out, changed := repairMismatchedClosers(in)
	if !changed || out != `{"a":[{"s":"x}]\"]}"}]}` {
		t.Fatalf("repair = %q, %v", out, changed)
	}
}

// TestTextToolCallRecoveryExecutesS329Wrapper drives the S329 text through a
// real turn: the bashy call executes exactly once on the model-tool path, the
// recovery event journals the closer repair, and the turn ends on DONE.
func TestTextToolCallRecoveryExecutesS329Wrapper(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	final := "DONE: added report()."
	fake, bashy, delivery, eventPath := runScriptedRecoveryTurn(t, root, "s329-wrapper", [][]provider.Event{
		completedText(s329WrapperText),
		completedText(final),
	})
	if got := fake.callCount(); got != 2 {
		t.Fatalf("model calls = %d, want the text call plus DONE", got)
	}
	matched := 0
	for _, script := range bashy.executed() {
		if script == s329WrapperScript {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("S329 script executed %d times, want exactly once", matched)
	}
	if got := countEventsOfType(t, eventPath, sessionStage.ToolCallCompleted); got != 1 {
		t.Fatalf("tool.call.completed events = %d, want the one recovered call", got)
	}
	if delivery.last != final {
		t.Fatalf("delivered %q, want the DONE completion", delivery.last)
	}
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	repairedEvents := 0
	for _, item := range replayed {
		if item.Type != textToolCallRecoveredEvent {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(item.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data["call_count"] != float64(1) || data["repaired_closers"] != true {
			t.Fatalf("recovery event = %#v, want call_count 1 with repaired_closers", data)
		}
		repairedEvents++
	}
	if repairedEvents != 1 {
		t.Fatalf("recovery events = %d, want exactly one", repairedEvents)
	}
}

// TestTextToolCallRecoveryRefusesMalformedWrapper proves a wrapper naming an
// undeclared tool still executes nothing after the repair: the allowlist
// holds and the text ends the turn unchanged.
func TestTextToolCallRecoveryRefusesMalformedWrapper(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	body := `{"tool_calls":[{"name":"shell","arguments":{"script":"rm -rf ."}]}`
	fake, bashy, delivery, eventPath := runScriptedRecoveryTurn(t, root, "s329-refusal", [][]provider.Event{
		completedText(body),
	})
	if got := fake.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want one: the refused text ends the turn", got)
	}
	for _, script := range bashy.executed() {
		if script == "rm -rf ." {
			t.Fatal("the undeclared tool's script executed")
		}
	}
	if got := countEventsOfType(t, eventPath, sessionStage.ToolCallCompleted); got != 0 {
		t.Fatalf("tool.call.completed events = %d, want none", got)
	}
	if got := countEventsOfType(t, eventPath, textToolCallRecoveredEvent); got != 0 {
		t.Fatalf("recovery events = %d, want none", got)
	}
	if delivery.last != body {
		t.Fatalf("delivered %q, want the body back untouched", delivery.last)
	}
}
