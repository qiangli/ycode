package turn

import "testing"

func TestS682GateReviewReplaysBareModelToolInvocation(t *testing.T) {
	const input = "<invoke name=\"bashy\">\n" +
		"<parameter name=\"script\">printf '%s\\n' 's682-replay'</parameter>\n" +
		"</invoke>"
	calls, ok := recoverTextToolCalls(input)
	if !ok || len(calls) != 1 {
		t.Fatalf("expected one normalized tool call, got %d (ok=%v)", len(calls), ok)
	}
}

func TestS682GateReviewAcceptsOnlyCompleteScriptFrames(t *testing.T) {
	const frame = "<invoke name=\"bashy\"><parameter name=\"script\">one &amp;&amp; two</parameter></invoke>"
	calls, ok := recoverClaudeDoorInvokes(frame + "\n" + frame)
	if !ok || len(calls) != 2 {
		t.Fatalf("expected both complete frames, got %d (ok=%v)", len(calls), ok)
	}
	first := calls[0].(map[string]any)
	if script := first["input"].(map[string]any)["script"]; script != "one && two" {
		t.Fatalf("wrong script: %q", script)
	}

	for _, text := range []string{
		"Example only: " + frame,
		frame + " ordinary final text",
		frame + "</invoke>",
		frame + "<invoke",
		"<invoke name=\"other\"><parameter name=\"script\">one</parameter></invoke>",
		"<invoke name=\"bashy\"><parameter name=\"script\"><b>one</b></parameter></invoke>",
		"<invoke name=\"bashy\"><parameter name=\"script\">  </parameter></invoke>",
		"<invoke name=\"bashy\"><parameter name=\"script\">one</parameter><parameter name=\"script\">two</parameter></invoke>",
	} {
		if _, ok := recoverClaudeDoorInvokes(text); ok {
			t.Errorf("accepted invalid invocation envelope %q", text)
		}
	}
}
