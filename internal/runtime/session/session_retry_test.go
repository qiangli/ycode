package session

import "testing"

func TestRemoveLastTurn(t *testing.T) {
	s := &Session{}

	// Add a user message then assistant response.
	s.Messages = append(s.Messages,
		ConversationMessage{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		ConversationMessage{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi there"}}},
	)

	removed := s.RemoveLastTurn()
	if removed != 2 {
		t.Errorf("expected 2 messages removed, got %d", removed)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected 0 messages remaining, got %d", len(s.Messages))
	}
}

func TestRemoveLastTurnMultiMessage(t *testing.T) {
	s := &Session{}

	// User -> Assistant -> User -> Assistant (with tool results)
	s.Messages = append(s.Messages,
		ConversationMessage{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "first"}}},
		ConversationMessage{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "reply 1"}}},
		ConversationMessage{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "second"}}},
		ConversationMessage{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeToolUse, Name: "bash"}}},
		ConversationMessage{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeToolResult, Content: "output"}}},
		ConversationMessage{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "reply 2"}}},
	)

	removed := s.RemoveLastTurn()
	// Should remove: reply 2, tool result, tool use, "second"
	if removed != 4 {
		t.Errorf("expected 4 messages removed, got %d", removed)
	}
	// Should have first exchange remaining.
	if len(s.Messages) != 2 {
		t.Errorf("expected 2 messages remaining, got %d", len(s.Messages))
	}
}

func TestRemoveLastTurnEmpty(t *testing.T) {
	s := &Session{}
	removed := s.RemoveLastTurn()
	if removed != 0 {
		t.Errorf("expected 0 messages removed from empty session, got %d", removed)
	}
}

func TestLastUserMessage(t *testing.T) {
	s := &Session{}
	s.Messages = append(s.Messages,
		ConversationMessage{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "first"}}},
		ConversationMessage{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "reply"}}},
		ConversationMessage{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "second"}}},
		ConversationMessage{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "reply 2"}}},
	)

	last := s.LastUserMessage()
	if last != "second" {
		t.Errorf("expected 'second', got %q", last)
	}
}

func TestLastUserMessageEmpty(t *testing.T) {
	s := &Session{}
	last := s.LastUserMessage()
	if last != "" {
		t.Errorf("expected empty, got %q", last)
	}
}
