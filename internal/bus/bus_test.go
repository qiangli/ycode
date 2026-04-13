package bus

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryBus_PublishSubscribe(t *testing.T) {
	b := NewMemoryBus()
	defer b.Close()

	ch, unsub := b.Subscribe()
	defer unsub()

	b.Publish(Event{
		Type:      EventTextDelta,
		SessionID: "s1",
		Data:      json.RawMessage(`{"text":"hello"}`),
	})

	select {
	case ev := <-ch:
		if ev.Type != EventTextDelta {
			t.Errorf("got type %q, want %q", ev.Type, EventTextDelta)
		}
		if ev.SessionID != "s1" {
			t.Errorf("got session %q, want %q", ev.SessionID, "s1")
		}
		if ev.ID == 0 {
			t.Error("event ID should be assigned")
		}
		if ev.Timestamp.IsZero() {
			t.Error("timestamp should be assigned")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMemoryBus_FilteredSubscribe(t *testing.T) {
	b := NewMemoryBus()
	defer b.Close()

	ch, unsub := b.Subscribe(EventTextDelta, EventTurnComplete)
	defer unsub()

	// Publish matching event.
	b.Publish(Event{Type: EventTextDelta, SessionID: "s1"})
	// Publish non-matching event.
	b.Publish(Event{Type: EventToolProgress, SessionID: "s1"})
	// Publish another matching event.
	b.Publish(Event{Type: EventTurnComplete, SessionID: "s1"})

	// Should receive exactly 2 events.
	received := drainEvents(ch, 2, time.Second)
	if len(received) != 2 {
		t.Fatalf("got %d events, want 2", len(received))
	}
	if received[0].Type != EventTextDelta {
		t.Errorf("first event type = %q, want %q", received[0].Type, EventTextDelta)
	}
	if received[1].Type != EventTurnComplete {
		t.Errorf("second event type = %q, want %q", received[1].Type, EventTurnComplete)
	}
}

func TestMemoryBus_Unsubscribe(t *testing.T) {
	b := NewMemoryBus()
	defer b.Close()

	ch, unsub := b.Subscribe()

	b.Publish(Event{Type: EventTextDelta, SessionID: "s1"})
	<-ch // consume

	unsub()

	// Channel should be closed after unsubscribe.
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestMemoryBus_FanOut(t *testing.T) {
	b := NewMemoryBus()
	defer b.Close()

	ch1, unsub1 := b.Subscribe()
	defer unsub1()
	ch2, unsub2 := b.Subscribe()
	defer unsub2()

	b.Publish(Event{Type: EventTextDelta, SessionID: "s1"})

	// Both subscribers should receive the event.
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("subscriber 1 timed out")
	}
	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("subscriber 2 timed out")
	}
}

func TestMemoryBus_SlowConsumer(t *testing.T) {
	b := NewMemoryBus()
	defer b.Close()

	ch, unsub := b.Subscribe()
	defer unsub()

	// Fill the buffer.
	for i := 0; i < defaultBufferSize+10; i++ {
		b.Publish(Event{Type: EventTextDelta, SessionID: "s1"})
	}

	// Should not block — extra events are dropped.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != defaultBufferSize {
		t.Errorf("got %d events, want %d (buffer size)", count, defaultBufferSize)
	}
}

func TestMemoryBus_Replay(t *testing.T) {
	b := NewMemoryBus()
	defer b.Close()

	// Publish some events.
	for i := 0; i < 5; i++ {
		b.Publish(Event{Type: EventTextDelta, SessionID: "s1"})
	}

	// Get all events from ring.
	events := b.Replay(0)
	if len(events) != 5 {
		t.Errorf("replay got %d events, want 5", len(events))
	}

	// Replay after specific ID.
	if len(events) > 2 {
		afterID := events[2].ID
		partial := b.Replay(afterID)
		if len(partial) != 2 {
			t.Errorf("partial replay got %d events, want 2", len(partial))
		}
	}
}

func drainEvents(ch <-chan Event, n int, timeout time.Duration) []Event {
	var events []Event
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(events) < n {
		select {
		case ev := <-ch:
			events = append(events, ev)
		case <-timer.C:
			return events
		}
	}
	return events
}
