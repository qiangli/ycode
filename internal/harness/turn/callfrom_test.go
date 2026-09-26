package turn

import (
	"strings"
	"testing"
)

// A malformed model call (another tool name, no script) is carried with the
// reason instead of failing the turn; a call without an id still fails.
func TestCallFromMarksInvalidCalls(t *testing.T) {
	call, err := callFrom(map[string]any{"id": "c1", "name": "python", "input": map[string]any{"script": "print(1)"}})
	if err != nil || !strings.Contains(call.Invalid, `no tool "python"`) {
		t.Fatalf("wrong tool: %+v %v", call, err)
	}
	call, err = callFrom(map[string]any{"id": "c2", "name": "bashy", "input": map[string]any{"script": "  "}})
	if err != nil || !strings.Contains(call.Invalid, "no script") {
		t.Fatalf("empty script: %+v %v", call, err)
	}
	call, err = callFrom(map[string]any{"id": "c3", "name": "bashy", "input": map[string]any{"script": "ls"}})
	if err != nil || call.Invalid != "" {
		t.Fatalf("valid call: %+v %v", call, err)
	}
	if _, err := callFrom(map[string]any{"name": "bashy", "input": map[string]any{"script": "ls"}}); err == nil {
		t.Fatal("a call without an id must fail")
	}
}
