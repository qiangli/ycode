package bus

import (
	"sync"
	"time"
)

const (
	// defaultBufferSize is the channel buffer for each subscriber.
	defaultBufferSize = 256

	// defaultRingSize is the number of recent events kept for replay.
	defaultRingSize = 1024
)

// subscriber is a single listener attached to the bus.
type subscriber struct {
	ch     chan Event
	filter map[EventType]struct{} // nil means accept all
}

func (s *subscriber) matches(t EventType) bool {
	if len(s.filter) == 0 {
		return true
	}
	_, ok := s.filter[t]
	return ok
}

// MemoryBus is an in-process fan-out event bus backed by Go channels.
type MemoryBus struct {
	mu   sync.RWMutex
	subs []*subscriber

	// Ring buffer for reconnection replay.
	ring    []Event
	ringIdx int
	ringMu  sync.RWMutex
}

// NewMemoryBus creates a new in-process event bus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		ring: make([]Event, defaultRingSize),
	}
}

// Publish sends an event to all matching subscribers.
// Slow consumers are skipped (non-blocking send).
func (b *MemoryBus) Publish(event Event) {
	if event.ID == 0 {
		event.ID = NextEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Store in ring buffer.
	b.ringMu.Lock()
	b.ring[b.ringIdx%defaultRingSize] = event
	b.ringIdx++
	b.ringMu.Unlock()

	// Fan out to subscribers.
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subs {
		if sub.matches(event.Type) {
			select {
			case sub.ch <- event:
			default:
				// Slow consumer — drop event to avoid blocking.
			}
		}
	}
}

// Subscribe returns a channel that receives matching events and an
// unsubscribe function. If no filter types are given, all events are
// delivered. The channel is closed when unsubscribe is called.
func (b *MemoryBus) Subscribe(filter ...EventType) (<-chan Event, func()) {
	sub := &subscriber{
		ch: make(chan Event, defaultBufferSize),
	}
	if len(filter) > 0 {
		sub.filter = make(map[EventType]struct{}, len(filter))
		for _, t := range filter {
			sub.filter[t] = struct{}{}
		}
	}

	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()

	unsubscribe := func() {
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

// Replay returns events from the ring buffer with ID > afterID.
// Useful for reconnecting clients that missed events.
func (b *MemoryBus) Replay(afterID uint64) []Event {
	b.ringMu.RLock()
	defer b.ringMu.RUnlock()

	var events []Event
	n := b.ringIdx
	if n > defaultRingSize {
		n = defaultRingSize
	}
	start := b.ringIdx - n
	for i := start; i < b.ringIdx; i++ {
		ev := b.ring[i%defaultRingSize]
		if ev.ID > afterID {
			events = append(events, ev)
		}
	}
	return events
}

// Close shuts down the bus and closes all subscriber channels.
func (b *MemoryBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		close(sub.ch)
	}
	b.subs = nil
	return nil
}
