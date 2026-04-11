package cli

import (
	"encoding/json"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/session"
)

func TestSessionMessages_CopiesAllFields(t *testing.T) {
	app := &App{
		session: &session.Session{
			Messages: []session.ConversationMessage{
				{
					Role: session.RoleAssistant,
					Content: []session.ContentBlock{
						{
							Type:  session.ContentTypeToolUse,
							ID:    "tool_abc123",
							Name:  "read_file",
							Input: json.RawMessage(`{"path":"/tmp/test.go"}`),
						},
					},
				},
				{
					Role: session.RoleUser,
					Content: []session.ContentBlock{
						{
							Type:      session.ContentTypeToolResult,
							ToolUseID: "tool_abc123",
							Content:   "file contents here",
							IsError:   false,
						},
					},
				},
				{
					Role: session.RoleUser,
					Content: []session.ContentBlock{
						{
							Type: session.ContentTypeText,
							Text: "Now fix the bug",
						},
					},
				},
			},
		},
	}

	msgs := app.sessionMessages()

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Check assistant tool_use block preserves all fields.
	toolUse := msgs[0].Content[0]
	if toolUse.Type != api.ContentTypeToolUse {
		t.Errorf("expected tool_use type, got %s", toolUse.Type)
	}
	if toolUse.ID != "tool_abc123" {
		t.Errorf("expected ID 'tool_abc123', got %q", toolUse.ID)
	}
	if toolUse.Name != "read_file" {
		t.Errorf("expected Name 'read_file', got %q", toolUse.Name)
	}
	if string(toolUse.Input) != `{"path":"/tmp/test.go"}` {
		t.Errorf("expected Input preserved, got %q", string(toolUse.Input))
	}

	// Check user tool_result block preserves all fields.
	toolResult := msgs[1].Content[0]
	if toolResult.Type != api.ContentTypeToolResult {
		t.Errorf("expected tool_result type, got %s", toolResult.Type)
	}
	if toolResult.ToolUseID != "tool_abc123" {
		t.Errorf("expected ToolUseID 'tool_abc123', got %q", toolResult.ToolUseID)
	}
	if toolResult.Content != "file contents here" {
		t.Errorf("expected Content preserved, got %q", toolResult.Content)
	}

	// Check text block.
	textBlock := msgs[2].Content[0]
	if textBlock.Text != "Now fix the bug" {
		t.Errorf("expected Text preserved, got %q", textBlock.Text)
	}
}
