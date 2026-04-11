package conversation

import (
	"encoding/json"
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

func TestSanitizeUserMessageForFlush(t *testing.T) {
	t.Run("removes tool_result blocks and keeps text", func(t *testing.T) {
		msg := api.Message{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_123",
					Content:   "file contents",
				},
				{
					Type: api.ContentTypeText,
					Text: "Now fix the bug",
				},
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_456",
					Content:   "other result",
				},
			},
		}

		result := sanitizeUserMessageForFlush(msg)

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 block, got %d", len(result.Content))
		}
		if result.Content[0].Type != api.ContentTypeText {
			t.Errorf("expected text block, got %s", result.Content[0].Type)
		}
		if result.Content[0].Text != "Now fix the bug" {
			t.Errorf("expected text preserved, got %q", result.Content[0].Text)
		}
		if result.Role != api.RoleUser {
			t.Errorf("expected user role, got %s", result.Role)
		}
	})

	t.Run("fallback when all blocks are tool_results", func(t *testing.T) {
		msg := api.Message{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_123",
					Content:   "result 1",
				},
				{
					Type:      api.ContentTypeToolResult,
					ToolUseID: "tool_456",
					Content:   "result 2",
				},
			},
		}

		result := sanitizeUserMessageForFlush(msg)

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 fallback block, got %d", len(result.Content))
		}
		if result.Content[0].Type != api.ContentTypeText {
			t.Errorf("expected text block, got %s", result.Content[0].Type)
		}
		if result.Content[0].Text != "Please continue from where we left off." {
			t.Errorf("unexpected fallback text: %q", result.Content[0].Text)
		}
	})

	t.Run("preserves non-tool-result blocks", func(t *testing.T) {
		msg := api.Message{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{
					Type: api.ContentTypeText,
					Text: "hello",
				},
				{
					Type:  api.ContentTypeToolUse,
					ID:    "t1",
					Name:  "bash",
					Input: json.RawMessage(`{"cmd":"ls"}`),
				},
			},
		}

		result := sanitizeUserMessageForFlush(msg)

		if len(result.Content) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(result.Content))
		}
		if result.Content[0].Text != "hello" {
			t.Errorf("expected text preserved, got %q", result.Content[0].Text)
		}
		if result.Content[1].ID != "t1" {
			t.Errorf("expected tool use ID preserved, got %q", result.Content[1].ID)
		}
	})
}
