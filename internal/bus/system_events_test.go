package bus

import (
	"testing"
)

func TestSystemEventQueue_EnqueueAndDrain(t *testing.T) {
	q := NewSystemEventQueue(5)

	q.Enqueue("sess-1", SystemEvent{Text: "hello", Level: "info"})
	q.Enqueue("sess-1", SystemEvent{Text: "world", Level: "info"})

	events := q.Drain("sess-1")
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Text != "hello" || events[1].Text != "world" {
		t.Error("unexpected event content")
	}

	// Drain again should be empty.
	events = q.Drain("sess-1")
	if len(events) != 0 {
		t.Errorf("expected 0 after drain, got %d", len(events))
	}
}

func TestSystemEventQueue_Dedup(t *testing.T) {
	q := NewSystemEventQueue(10)

	q.Enqueue("sess-1", SystemEvent{Text: "same", Level: "info"})
	q.Enqueue("sess-1", SystemEvent{Text: "same", Level: "info"})
	q.Enqueue("sess-1", SystemEvent{Text: "same", Level: "info"})

	events := q.Drain("sess-1")
	if len(events) != 1 {
		t.Errorf("dedup should keep only 1 event, got %d", len(events))
	}
}

func TestSystemEventQueue_RingBuffer(t *testing.T) {
	q := NewSystemEventQueue(3)

	q.Enqueue("sess-1", SystemEvent{Text: "a", Level: "info"})
	q.Enqueue("sess-1", SystemEvent{Text: "b", Level: "info"})
	q.Enqueue("sess-1", SystemEvent{Text: "c", Level: "info"})
	q.Enqueue("sess-1", SystemEvent{Text: "d", Level: "info"}) // evicts "a"

	events := q.Peek("sess-1")
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Text != "b" {
		t.Errorf("expected oldest to be 'b', got %q", events[0].Text)
	}
	if events[2].Text != "d" {
		t.Errorf("expected newest to be 'd', got %q", events[2].Text)
	}
}

func TestSystemEventQueue_Peek(t *testing.T) {
	q := NewSystemEventQueue(5)

	q.Enqueue("sess-1", SystemEvent{Text: "peek", Level: "info"})

	events := q.Peek("sess-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Peek should not drain.
	if q.Len("sess-1") != 1 {
		t.Error("peek should not drain events")
	}
}

func TestSystemEventQueue_Clear(t *testing.T) {
	q := NewSystemEventQueue(5)

	q.Enqueue("sess-1", SystemEvent{Text: "clear", Level: "info"})
	q.Clear("sess-1")

	if q.Len("sess-1") != 0 {
		t.Error("expected 0 after clear")
	}
}

func TestSystemEventQueue_MultiSession(t *testing.T) {
	q := NewSystemEventQueue(5)

	q.Enqueue("sess-1", SystemEvent{Text: "one", Level: "info"})
	q.Enqueue("sess-2", SystemEvent{Text: "two", Level: "info"})

	if q.Len("sess-1") != 1 {
		t.Errorf("sess-1: expected 1, got %d", q.Len("sess-1"))
	}
	if q.Len("sess-2") != 1 {
		t.Errorf("sess-2: expected 1, got %d", q.Len("sess-2"))
	}

	// Draining one doesn't affect the other.
	q.Drain("sess-1")
	if q.Len("sess-2") != 1 {
		t.Error("draining sess-1 should not affect sess-2")
	}
}

func TestSystemEventQueue_EmptySession(t *testing.T) {
	q := NewSystemEventQueue(5)

	if events := q.Drain("nonexistent"); events != nil {
		t.Errorf("expected nil for nonexistent session, got %v", events)
	}
	if q.Len("nonexistent") != 0 {
		t.Error("expected 0 for nonexistent session")
	}
}
