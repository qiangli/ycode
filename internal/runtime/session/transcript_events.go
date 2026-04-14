package session

import (
	"encoding/json"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// TranscriptEventType identifies transcript event subtypes.
type TranscriptEventType string

const (
	TranscriptMessageAdded   TranscriptEventType = "message_added"
	TranscriptSessionCleared TranscriptEventType = "session_cleared"
)

// TranscriptEvent is the payload for transcript update events.
type TranscriptEvent struct {
	Type      TranscriptEventType `json:"type"`
	SessionID string              `json:"session_id"`
	MessageID string              `json:"message_id,omitempty"`
	Role      string              `json:"role,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
}

// TranscriptNotifier emits bus events when the session transcript changes.
type TranscriptNotifier struct {
	bus       bus.Bus
	sessionID string
}

// NewTranscriptNotifier creates a notifier for the given session.
func NewTranscriptNotifier(b bus.Bus, sessionID string) *TranscriptNotifier {
	return &TranscriptNotifier{
		bus:       b,
		sessionID: sessionID,
	}
}

// NotifyMessageAdded emits a transcript update event for a new message.
func (tn *TranscriptNotifier) NotifyMessageAdded(msg ConversationMessage) {
	evt := TranscriptEvent{
		Type:      TranscriptMessageAdded,
		SessionID: tn.sessionID,
		MessageID: msg.UUID,
		Role:      string(msg.Role),
		Timestamp: msg.Timestamp,
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	data, _ := json.Marshal(evt)
	tn.bus.Publish(bus.Event{
		ID:        bus.NextEventID(),
		Type:      bus.EventTranscriptUpdate,
		SessionID: tn.sessionID,
		Timestamp: evt.Timestamp,
		Data:      data,
	})
}

// NotifySessionCleared emits a transcript clear event.
func (tn *TranscriptNotifier) NotifySessionCleared() {
	evt := TranscriptEvent{
		Type:      TranscriptSessionCleared,
		SessionID: tn.sessionID,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(evt)
	tn.bus.Publish(bus.Event{
		ID:        bus.NextEventID(),
		Type:      bus.EventTranscriptUpdate,
		SessionID: tn.sessionID,
		Timestamp: evt.Timestamp,
		Data:      data,
	})
}
