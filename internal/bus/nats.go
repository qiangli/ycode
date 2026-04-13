package bus

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// natsSubjectPrefix is the root subject for all ycode events.
	natsSubjectPrefix = "ycode.sessions"

	// natsInputSuffix is the subject suffix for client-to-server commands.
	natsInputSuffix = "input"

	// natsEventsSuffix is the subject suffix for server-to-client events.
	natsEventsSuffix = "events"
)

// NATSBus implements Bus using NATS as the transport layer.
// Events are published to subjects like: ycode.sessions.{session_id}.events.{type}
// Client commands arrive on: ycode.sessions.{session_id}.input
type NATSBus struct {
	conn   *nats.Conn
	logger *slog.Logger

	mu   sync.Mutex
	subs []*natsSub
}

type natsSub struct {
	ch       chan Event
	filter   map[EventType]struct{}
	natsSubs []*nats.Subscription
}

func (s *natsSub) matches(t EventType) bool {
	if len(s.filter) == 0 {
		return true
	}
	_, ok := s.filter[t]
	return ok
}

// NewNATSBus creates a bus backed by a NATS connection.
func NewNATSBus(conn *nats.Conn) *NATSBus {
	return &NATSBus{
		conn:   conn,
		logger: slog.Default(),
	}
}

// EventSubject returns the NATS subject for a given session and event type.
func EventSubject(sessionID string, eventType EventType) string {
	return fmt.Sprintf("%s.%s.%s.%s", natsSubjectPrefix, sessionID, natsEventsSuffix, eventType)
}

// SessionEventsSubject returns the wildcard subject for all events of a session.
func SessionEventsSubject(sessionID string) string {
	return fmt.Sprintf("%s.%s.%s.>", natsSubjectPrefix, sessionID, natsEventsSuffix)
}

// InputSubject returns the NATS subject for client commands to a session.
func InputSubject(sessionID string) string {
	return fmt.Sprintf("%s.%s.%s", natsSubjectPrefix, sessionID, natsInputSuffix)
}

// AllInputSubject returns the wildcard subject for all client commands.
func AllInputSubject() string {
	return fmt.Sprintf("%s.*.%s", natsSubjectPrefix, natsInputSuffix)
}

// Publish sends an event to NATS and any local subscribers.
func (b *NATSBus) Publish(event Event) {
	if event.ID == 0 {
		event.ID = NextEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("failed to marshal event", "error", err)
		return
	}

	subject := EventSubject(event.SessionID, event.Type)
	if err := b.conn.Publish(subject, data); err != nil {
		b.logger.Error("failed to publish to NATS", "subject", subject, "error", err)
	}
}

// Subscribe returns a channel that receives events from NATS.
// If filter types are provided, only matching events are delivered.
func (b *NATSBus) Subscribe(filter ...EventType) (<-chan Event, func()) {
	sub := &natsSub{
		ch: make(chan Event, defaultBufferSize),
	}
	if len(filter) > 0 {
		sub.filter = make(map[EventType]struct{}, len(filter))
		for _, t := range filter {
			sub.filter[t] = struct{}{}
		}
	}

	// Build NATS subscription subjects based on filters.
	var subjects []string
	if len(filter) > 0 {
		for _, t := range filter {
			// Subscribe to all sessions for this event type.
			subjects = append(subjects, fmt.Sprintf("%s.*.%s.%s", natsSubjectPrefix, natsEventsSuffix, t))
		}
	} else {
		// Subscribe to all events across all sessions.
		subjects = append(subjects, fmt.Sprintf("%s.>", natsSubjectPrefix))
	}

	msgHandler := func(msg *nats.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.logger.Error("failed to unmarshal NATS event", "error", err)
			return
		}
		// Double-check filter match (NATS wildcards might be broader).
		if !sub.matches(event.Type) {
			return
		}
		select {
		case sub.ch <- event:
		default:
			// Slow consumer — drop.
		}
	}

	for _, subj := range subjects {
		ns, err := b.conn.Subscribe(subj, msgHandler)
		if err != nil {
			b.logger.Error("failed to subscribe to NATS", "subject", subj, "error", err)
			continue
		}
		sub.natsSubs = append(sub.natsSubs, ns)
	}

	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()

	unsubscribe := func() {
		for _, ns := range sub.natsSubs {
			_ = ns.Unsubscribe()
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s == sub {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				close(sub.ch)
				return
			}
		}
	}

	return sub.ch, unsubscribe
}

// SubscribeInput subscribes to client commands for all sessions.
// Returns a channel of events and an unsubscribe function.
func (b *NATSBus) SubscribeInput() (<-chan Event, func()) {
	ch := make(chan Event, defaultBufferSize)

	ns, err := b.conn.Subscribe(AllInputSubject(), func(msg *nats.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.logger.Error("failed to unmarshal NATS input", "error", err)
			return
		}
		// Extract session ID from subject: ycode.sessions.{id}.input
		parts := strings.Split(msg.Subject, ".")
		if len(parts) >= 3 {
			event.SessionID = parts[2]
		}
		select {
		case ch <- event:
		default:
		}
	})
	if err != nil {
		b.logger.Error("failed to subscribe to NATS input", "error", err)
		close(ch)
		return ch, func() {}
	}

	return ch, func() {
		_ = ns.Unsubscribe()
		close(ch)
	}
}

// PublishInput publishes a client command to a session's input subject.
func (b *NATSBus) PublishInput(sessionID string, event Event) error {
	if event.ID == 0 {
		event.ID = NextEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal input event: %w", err)
	}
	return b.conn.Publish(InputSubject(sessionID), data)
}

// Close unsubscribes all listeners. Does not close the NATS connection
// (the caller owns the connection lifecycle).
func (b *NATSBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		for _, ns := range sub.natsSubs {
			_ = ns.Unsubscribe()
		}
		close(sub.ch)
	}
	b.subs = nil
	return nil
}
