package session

import "testing"

func TestGenerateDefaultTitle(t *testing.T) {
	s := &Session{}
	s.Messages = append(s.Messages,
		ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "Fix the login bug in auth.go"}},
		},
	)

	title := s.GenerateDefaultTitle()
	if title != "Fix the login bug in auth.go" {
		t.Errorf("expected title from first message, got %q", title)
	}
	if s.Title != title {
		t.Error("expected title to be set on session")
	}
}

func TestGenerateDefaultTitleTruncate(t *testing.T) {
	s := &Session{}
	longMsg := "This is a very long message that exceeds fifty characters and should be truncated properly"
	s.Messages = append(s.Messages,
		ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: longMsg}},
		},
	)

	title := s.GenerateDefaultTitle()
	if len(title) > 50 {
		t.Errorf("title too long: %d chars", len(title))
	}
	if title[len(title)-3:] != "..." {
		t.Error("expected truncated title to end with ...")
	}
}

func TestGenerateDefaultTitleNewline(t *testing.T) {
	s := &Session{}
	s.Messages = append(s.Messages,
		ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "First line\nSecond line"}},
		},
	)

	title := s.GenerateDefaultTitle()
	if title != "First line" {
		t.Errorf("expected title truncated at newline, got %q", title)
	}
}

func TestGenerateDefaultTitleEmpty(t *testing.T) {
	s := &Session{}
	title := s.GenerateDefaultTitle()
	if title != "" {
		t.Errorf("expected empty title for empty session, got %q", title)
	}
}

func TestSetTitle(t *testing.T) {
	s := &Session{}
	s.SetTitle("Custom Title")
	if s.Title != "Custom Title" {
		t.Errorf("expected 'Custom Title', got %q", s.Title)
	}
}
