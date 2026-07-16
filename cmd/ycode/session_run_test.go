package main

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/session"
)

func TestCreateSessionForRunForkInheritsContext(t *testing.T) {
	sessionDir := t.TempDir()
	sourceID := "source-session"
	source, err := session.NewWithID(sessionDir, sourceID)
	if err != nil {
		t.Fatalf("create source session: %v", err)
	}
	sourceMessages := []session.ConversationMessage{
		{
			UUID:      "msg-1",
			Role:      session.RoleUser,
			Content:   []session.ContentBlock{{Type: session.ContentTypeText, Text: "first"}},
			Timestamp: time.Unix(100, 0),
		},
		{
			UUID:      "msg-2",
			Role:      session.RoleAssistant,
			Content:   []session.ContentBlock{{Type: session.ContentTypeText, Text: "second"}},
			Timestamp: time.Unix(101, 0),
			Model:     "test-model",
		},
		{
			UUID:      "msg-3",
			Role:      session.RoleUser,
			Content:   []session.ContentBlock{{Type: session.ContentTypeText, Text: "third"}},
			Timestamp: time.Unix(102, 0),
		},
	}
	for _, msg := range sourceMessages {
		if err := source.AddMessage(msg); err != nil {
			t.Fatalf("seed source message: %v", err)
		}
	}

	forked, err := createSessionForRun(sessionDir, "fork-session", sourceID, "")
	if err != nil {
		t.Fatalf("fork session: %v", err)
	}
	if forked.ID == sourceID {
		t.Fatalf("fork reused source ID %q", sourceID)
	}
	if forked.ID != "fork-session" {
		t.Fatalf("got fork ID %q, want %q", forked.ID, "fork-session")
	}
	assertMessagesEqual(t, forked.Messages, sourceMessages)

	reloadedSource, err := session.Load(sessionDir, sourceID)
	if err != nil {
		t.Fatalf("reload source: %v", err)
	}
	assertMessagesEqual(t, reloadedSource.Messages, sourceMessages)
}

func TestCreateSessionForRunResumeUsesSameSession(t *testing.T) {
	sessionDir := t.TempDir()
	source, err := session.NewWithID(sessionDir, "resume-session")
	if err != nil {
		t.Fatalf("create source session: %v", err)
	}
	msg := session.ConversationMessage{
		UUID:      "msg-1",
		Role:      session.RoleUser,
		Content:   []session.ContentBlock{{Type: session.ContentTypeText, Text: "hello"}},
		Timestamp: time.Unix(100, 0),
	}
	if err := source.AddMessage(msg); err != nil {
		t.Fatalf("seed source message: %v", err)
	}

	resumed, err := createSessionForRun(sessionDir, "ignored-new-id", " resume-session ", "resume-session")
	if err == nil {
		t.Fatal("expected conflicting fork/resume error")
	}

	resumed, err = createSessionForRun(sessionDir, "ignored-new-id", "", "resume-session")
	if err != nil {
		t.Fatalf("resume session: %v", err)
	}
	if resumed.ID != "resume-session" {
		t.Fatalf("got resumed ID %q, want %q", resumed.ID, "resume-session")
	}
	assertMessagesEqual(t, resumed.Messages, []session.ConversationMessage{msg})
}

func assertMessagesEqual(t *testing.T, got, want []session.ConversationMessage) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].UUID != want[i].UUID {
			t.Fatalf("message %d UUID = %q, want %q", i, got[i].UUID, want[i].UUID)
		}
		if got[i].Role != want[i].Role {
			t.Fatalf("message %d role = %q, want %q", i, got[i].Role, want[i].Role)
		}
		if got[i].Model != want[i].Model {
			t.Fatalf("message %d model = %q, want %q", i, got[i].Model, want[i].Model)
		}
		if !got[i].Timestamp.Equal(want[i].Timestamp) {
			t.Fatalf("message %d timestamp = %v, want %v", i, got[i].Timestamp, want[i].Timestamp)
		}
		if len(got[i].Content) != len(want[i].Content) {
			t.Fatalf("message %d got %d content blocks, want %d", i, len(got[i].Content), len(want[i].Content))
		}
		for j := range want[i].Content {
			if got[i].Content[j].Type != want[i].Content[j].Type ||
				got[i].Content[j].Text != want[i].Content[j].Text ||
				got[i].Content[j].ID != want[i].Content[j].ID ||
				got[i].Content[j].Name != want[i].Content[j].Name ||
				string(got[i].Content[j].Input) != string(want[i].Content[j].Input) ||
				got[i].Content[j].ToolUseID != want[i].Content[j].ToolUseID ||
				got[i].Content[j].Content != want[i].Content[j].Content ||
				got[i].Content[j].IsError != want[i].Content[j].IsError {
				t.Fatalf("message %d content block %d = %#v, want %#v", i, j, got[i].Content[j], want[i].Content[j])
			}
		}
	}
}
