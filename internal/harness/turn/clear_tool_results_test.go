package turn

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
)

func toolRoundTrip(id, result string) []message.Message {
	return []message.Message{
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: id, Name: "bashy"}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: id, Content: result}}},
	}
}

func clearToolResultsOutcome(t *testing.T, messages []message.Message, olderThanTurns int, placeholder string) []message.Message {
	t.Helper()
	r := &Runtime{}
	out := r.clearToolResults(context.Background(), pipeline.Invocation{
		Inputs: map[string]any{"state": map[string]any{"messages": messages}},
		With:   map[string]any{"olderThanTurns": olderThanTurns, "placeholder": placeholder},
	})
	if out.Err != nil {
		t.Fatalf("clearToolResults: %v", out.Err)
	}
	state := out.Outputs["state"].(map[string]any)
	result, err := messagesFrom(state["messages"])
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func toolResultContents(t *testing.T, messages []message.Message) []string {
	t.Helper()
	var got []string
	for _, m := range messages {
		for _, block := range m.Content {
			if block.Type == message.ContentTypeToolResult {
				got = append(got, block.Content)
			}
		}
	}
	return got
}

// Sprint 379 Story #53 (c88f66baf976): a live genie turn ran dozens of tool
// calls inside one agent-loop turn and, by the eighth model request, every
// earlier tool result had already been replaced by the placeholder — the
// model never saw its own command output again. clearToolResults counted
// "turns" by scanning backward for message.RoleUser, but a synthesized
// tool_result message also carries RoleUser, so one real conversational
// turn with many tool round trips was read as many turns and almost
// everything but the newest few results was cleared immediately.
func TestClearToolResultsCountsRealUserTurnsNotToolResultWrappers(t *testing.T) {
	messages := []message.Message{{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "start"}}}}
	for i := 0; i < 10; i++ {
		messages = append(messages, toolRoundTrip(string(rune('a'+i)), "result "+string(rune('a'+i)))...)
	}

	result := clearToolResultsOutcome(t, messages, 4, "[cleared]")

	for _, content := range toolResultContents(t, result) {
		if content == "[cleared]" {
			t.Fatalf("a tool result was cleared inside the single real turn still open: %#v", result)
		}
	}
}

// Clearing itself must still work: once there really are more than
// olderThanTurns real conversational turns, the oldest ones give way.
func TestClearToolResultsStillClearsOlderRealTurns(t *testing.T) {
	var messages []message.Message
	for turn := 0; turn < 6; turn++ {
		messages = append(messages, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "turn"}}})
		messages = append(messages, toolRoundTrip(string(rune('a'+turn)), "result "+string(rune('a'+turn)))...)
	}

	result := clearToolResultsOutcome(t, messages, 4, "[cleared]")

	contents := toolResultContents(t, result)
	if len(contents) != 6 {
		t.Fatalf("tool results = %#v, want 6", contents)
	}
	if contents[0] != "[cleared]" {
		t.Fatalf("oldest turn was not cleared: %#v", contents)
	}
	if contents[len(contents)-1] == "[cleared]" {
		t.Fatalf("newest turn was cleared: %#v", contents)
	}
}
