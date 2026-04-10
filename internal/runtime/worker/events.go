package worker

import (
	"fmt"
	"time"
)

// EventType identifies the kind of worker event.
type EventType string

const (
	EventSpawned       EventType = "spawned"
	EventTrustRequired EventType = "trust_required"
	EventTrustResolved EventType = "trust_resolved"
	EventReady         EventType = "ready"
	EventPromptSent    EventType = "prompt_sent"
	EventCompleted     EventType = "completed"
	EventFailed        EventType = "failed"
	EventRestarted     EventType = "restarted"
	EventTerminated    EventType = "terminated"
)

// Event records a worker lifecycle event.
type Event struct {
	Type      EventType `json:"type"`
	WorkerID  string    `json:"worker_id"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// EventLog tracks worker events.
type EventLog struct {
	events []Event
}

// NewEventLog creates a new event log.
func NewEventLog() *EventLog {
	return &EventLog{}
}

// Record adds an event to the log.
func (el *EventLog) Record(workerID string, eventType EventType, message string) {
	el.events = append(el.events, Event{
		Type:      eventType,
		WorkerID:  workerID,
		Timestamp: time.Now(),
		Message:   message,
	})
}

// Events returns all events, optionally filtered by worker ID.
func (el *EventLog) Events(workerID string) []Event {
	if workerID == "" {
		return el.events
	}
	var filtered []Event
	for _, e := range el.events {
		if e.WorkerID == workerID {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// RecoverPrompt handles prompt misdelivery by checking if a prompt was sent
// to a worker that failed before processing it.
func RecoverPrompt(registry *Registry, workerID string) (string, error) {
	w, ok := registry.Get(workerID)
	if !ok {
		return "", fmt.Errorf("worker %q not found", workerID)
	}

	if w.State == StateFailed && w.Prompt != "" {
		prompt := w.Prompt
		return prompt, nil
	}

	return "", fmt.Errorf("worker %q has no unprocessed prompt (state: %s)", workerID, w.State)
}
