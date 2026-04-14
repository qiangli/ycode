package bus

import (
	"sync"
	"time"
)

const (
	// DefaultMaxSystemEvents is the max events per session ring buffer.
	DefaultMaxSystemEvents = 20
)

// SystemEvent is a lightweight ephemeral event scoped to a session.
type SystemEvent struct {
	Text      string    `json:"text"`
	Level     string    `json:"level"` // "info", "warn", "error"
	Timestamp time.Time `json:"timestamp"`
}

// SystemEventQueue manages per-session ephemeral event ring buffers.
type SystemEventQueue struct {
	mu       sync.Mutex
	sessions map[string]*sessionRing
	maxSize  int
}

type sessionRing struct {
	events []SystemEvent
	size   int
}

// NewSystemEventQueue creates a new system event queue.
func NewSystemEventQueue(maxPerSession int) *SystemEventQueue {
	if maxPerSession <= 0 {
		maxPerSession = DefaultMaxSystemEvents
	}
	return &SystemEventQueue{
		sessions: make(map[string]*sessionRing),
		maxSize:  maxPerSession,
	}
}

// Enqueue adds an event to the session's ring buffer.
// Consecutive identical text events are deduplicated.
func (q *SystemEventQueue) Enqueue(sessionID string, event SystemEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	ring, ok := q.sessions[sessionID]
	if !ok {
		ring = &sessionRing{
			events: make([]SystemEvent, 0, q.maxSize),
			size:   q.maxSize,
		}
		q.sessions[sessionID] = ring
	}

	// Dedup: skip if last event has identical text.
	if len(ring.events) > 0 && ring.events[len(ring.events)-1].Text == event.Text {
		return
	}

	// Ring buffer: evict oldest if full.
	if len(ring.events) >= ring.size {
		ring.events = ring.events[1:]
	}
	ring.events = append(ring.events, event)
}

// Drain removes and returns all events for a session.
func (q *SystemEventQueue) Drain(sessionID string) []SystemEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	ring, ok := q.sessions[sessionID]
	if !ok || len(ring.events) == 0 {
		return nil
	}

	events := make([]SystemEvent, len(ring.events))
	copy(events, ring.events)
	ring.events = ring.events[:0]
	return events
}

// Peek returns events without removing them.
func (q *SystemEventQueue) Peek(sessionID string) []SystemEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	ring, ok := q.sessions[sessionID]
	if !ok || len(ring.events) == 0 {
		return nil
	}

	events := make([]SystemEvent, len(ring.events))
	copy(events, ring.events)
	return events
}

// Len returns the number of events for a session.
func (q *SystemEventQueue) Len(sessionID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	ring, ok := q.sessions[sessionID]
	if !ok {
		return 0
	}
	return len(ring.events)
}

// Clear removes all events for a session.
func (q *SystemEventQueue) Clear(sessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.sessions, sessionID)
}
