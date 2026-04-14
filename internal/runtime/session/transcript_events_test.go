package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestTranscriptNotifier_MessageAdded(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventTranscriptUpdate)
	defer unsub()

	notifier := NewTranscriptNotifier(mb, "sess-1")
	notifier.NotifyMessageAdded(ConversationMessage{
		UUID:    "msg-123",
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}},
	})

	select {
	case ev := <-ch:
		if ev.Type != bus.EventTranscriptUpdate {
			t.Errorf("expected transcript.update, got %s", ev.Type)
		}
		if ev.SessionID != "sess-1" {
			t.Errorf("expected sess-1, got %s", ev.SessionID)
		}
		var payload TranscriptEvent
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Type != TranscriptMessageAdded {
			t.Errorf("expected message_added, got %s", payload.Type)
		}
		if payload.MessageID != "msg-123" {
			t.Errorf("expected msg-123, got %s", payload.MessageID)
		}
		if payload.Role != string(RoleUser) {
			t.Errorf("expected user, got %s", payload.Role)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestTranscriptNotifier_SessionCleared(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventTranscriptUpdate)
	defer unsub()

	notifier := NewTranscriptNotifier(mb, "sess-2")
	notifier.NotifySessionCleared()

	select {
	case ev := <-ch:
		var payload TranscriptEvent
		json.Unmarshal(ev.Data, &payload)
		if payload.Type != TranscriptSessionCleared {
			t.Errorf("expected session_cleared, got %s", payload.Type)
		}
		if payload.SessionID != "sess-2" {
			t.Errorf("expected sess-2, got %s", payload.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}
