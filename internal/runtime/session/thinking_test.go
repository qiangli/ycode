package session

import (
	"strings"
	"testing"
)

func TestClearOldThinking_NoThinking(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "hello"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "hi"},
		}},
	}

	result, cleared := ClearOldThinking(messages, 2)
	if cleared != 0 {
		t.Errorf("expected 0 cleared, got %d", cleared)
	}
	if len(result) != len(messages) {
		t.Errorf("expected %d messages, got %d", len(messages), len(result))
	}
}

func TestClearOldThinking_KeepsRecent(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<thinking>old reasoning</thinking>"},
			{Type: ContentTypeText, Text: "response 1"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "question"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<thinking>recent reasoning</thinking>"},
			{Type: ContentTypeText, Text: "response 2"},
		}},
	}

	// Keep last 2 thinking turns — both have thinking, so nothing should be cleared.
	result, cleared := ClearOldThinking(messages, 2)
	if cleared != 0 {
		t.Errorf("expected 0 cleared (only 2 thinking turns, keeping 2), got %d", cleared)
	}
	// Verify original thinking is preserved.
	if result[0].Content[0].Text != "<thinking>old reasoning</thinking>" {
		t.Error("thinking should be preserved when within keep limit")
	}
}

func TestClearOldThinking_ClearsOld(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<thinking>very old</thinking>"},
			{Type: ContentTypeText, Text: "old response"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "q1"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<thinking>old reasoning</thinking>"},
			{Type: ContentTypeText, Text: "mid response"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "q2"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<thinking>recent reasoning</thinking>"},
			{Type: ContentTypeText, Text: "latest response"},
		}},
	}

	result, cleared := ClearOldThinking(messages, 1)
	if cleared != 2 {
		t.Errorf("expected 2 cleared (3 thinking turns, keeping 1), got %d", cleared)
	}

	// First assistant message: thinking should be cleared.
	if result[0].Content[0].Text != ThinkingClearedMarker {
		t.Errorf("old thinking should be cleared, got: %s", result[0].Content[0].Text)
	}
	// Non-thinking content preserved.
	if result[0].Content[1].Text != "old response" {
		t.Error("non-thinking content should be preserved")
	}

	// Last assistant message: thinking should be preserved.
	if result[4].Content[0].Text != "<thinking>recent reasoning</thinking>" {
		t.Error("recent thinking should be preserved")
	}
}

func TestClearOldThinking_AntThinking(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<antThinking>old analysis</antThinking>"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<antThinking>recent analysis</antThinking>"},
		}},
	}

	result, cleared := ClearOldThinking(messages, 1)
	if cleared != 1 {
		t.Errorf("expected 1 cleared, got %d", cleared)
	}
	if result[0].Content[0].Text != ThinkingClearedMarker {
		t.Error("old antThinking should be cleared")
	}
}

func TestClearOldThinking_ReasoningTag(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<reasoning>step by step analysis of the problem</reasoning>"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<reasoning>latest reasoning</reasoning>"},
		}},
	}

	result, cleared := ClearOldThinking(messages, 1)
	if cleared != 1 {
		t.Errorf("expected 1 cleared, got %d", cleared)
	}
	if result[0].Content[0].Text != ThinkingClearedMarker {
		t.Error("old reasoning should be cleared")
	}
}

func TestStripMediaBlocks_NoMedia(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "normal text"},
		}},
	}

	result, stripped := StripMediaBlocks(messages)
	if stripped != 0 {
		t.Errorf("expected 0 stripped, got %d", stripped)
	}
	if result[0].Content[0].Text != "normal text" {
		t.Error("text should be unchanged")
	}
}

func TestStripMediaBlocks_Image(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "data:image/png;base64,iVBOR..."},
			{Type: ContentTypeText, Text: "the screenshot shows an error"},
		}},
	}

	result, stripped := StripMediaBlocks(messages)
	if stripped != 1 {
		t.Errorf("expected 1 stripped, got %d", stripped)
	}
	if result[0].Content[0].Text != "[image]" {
		t.Errorf("image should be replaced with marker, got: %s", result[0].Content[0].Text)
	}
	if result[0].Content[1].Text != "the screenshot shows an error" {
		t.Error("non-image text should be preserved")
	}
}

func TestStripMediaBlocks_Document(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "[document: report.pdf]"},
		}},
	}

	result, stripped := StripMediaBlocks(messages)
	if stripped != 1 {
		t.Errorf("expected 1 stripped, got %d", stripped)
	}
	if result[0].Content[0].Text != "[document]" {
		t.Errorf("document should be replaced with marker, got: %s", result[0].Content[0].Text)
	}
}

func TestStripMediaBlocks_PreservesStructure(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "here's my analysis"},
			{Type: ContentTypeToolUse, Name: "bash", ID: "t1"},
		}},
	}

	result, stripped := StripMediaBlocks(messages)
	if stripped != 0 {
		t.Error("tool blocks should not be stripped")
	}
	if len(result[0].Content) != 2 {
		t.Error("content blocks should be preserved")
	}
}

func TestHasThinkingBlock(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"<thinking>analysis</thinking>", true},
		{"<antThinking>analysis</antThinking>", true},
		{"<reasoning>step by step</reasoning>", true},
		{"normal text", false},
		{"short", false},
		{"", false},
	}

	for _, tt := range tests {
		msg := ConversationMessage{
			Content: []ContentBlock{{Type: ContentTypeText, Text: tt.text}},
		}
		if got := hasThinkingBlock(msg); got != tt.want {
			t.Errorf("hasThinkingBlock(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestClearOldThinking_DoesNotMutateOriginal(t *testing.T) {
	originalText := "<thinking>important reasoning about the problem domain</thinking>"
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: originalText},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "<thinking>recent</thinking>"},
		}},
	}

	_, _ = ClearOldThinking(messages, 1)

	// Original should not be modified.
	if messages[0].Content[0].Text != originalText {
		t.Error("original messages should not be mutated")
	}
}

func TestStripMediaBlocks_ImagePrefix(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "[image: screenshot.png]"},
		}},
	}

	result, stripped := StripMediaBlocks(messages)
	if stripped != 1 {
		t.Errorf("expected 1 stripped, got %d", stripped)
	}
	if !strings.Contains(result[0].Content[0].Text, "[image]") {
		t.Error("should be replaced with [image] marker")
	}
}
