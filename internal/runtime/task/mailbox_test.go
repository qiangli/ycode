package task

import (
	"testing"
	"time"
)

func TestNewMailbox(t *testing.T) {
	m := NewMailbox(8)
	if m == nil {
		t.Fatal("NewMailbox returned nil")
	}
	if m.Pending() != 0 {
		t.Fatalf("new mailbox pending = %d, want 0", m.Pending())
	}
}

func TestNewMailboxZeroBuffer(t *testing.T) {
	m := NewMailbox(0)
	if m == nil {
		t.Fatal("NewMailbox(0) returned nil")
	}
	// Should default to 16.
	if cap(m.ch) != 16 {
		t.Fatalf("capacity = %d, want 16", cap(m.ch))
	}
}

func TestSendAndReceive(t *testing.T) {
	m := NewMailbox(4)
	msg := TaskMessage{From: "a", To: "b", Type: "result", Payload: "hello"}

	if err := m.Send(msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if m.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", m.Pending())
	}

	got, ok := m.Receive(time.Second)
	if !ok {
		t.Fatal("Receive returned false")
	}
	if got.Payload != "hello" {
		t.Fatalf("payload = %q, want %q", got.Payload, "hello")
	}
	if m.Pending() != 0 {
		t.Fatalf("pending after receive = %d, want 0", m.Pending())
	}
}

func TestSendFullMailbox(t *testing.T) {
	m := NewMailbox(1)
	msg := TaskMessage{From: "a", To: "b", Type: "result", Payload: "x"}

	if err := m.Send(msg); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	err := m.Send(msg)
	if err == nil {
		t.Fatal("expected error on full mailbox")
	}
}

func TestReceiveTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	m := NewMailbox(4)
	_, ok := m.Receive(10 * time.Millisecond)
	if ok {
		t.Fatal("expected timeout (false)")
	}
}

func TestReceiveZeroTimeout(t *testing.T) {
	m := NewMailbox(4)
	// Zero timeout with no message should return immediately.
	_, ok := m.Receive(0)
	if ok {
		t.Fatal("expected false with zero timeout and empty mailbox")
	}

	// With a message available, zero timeout should return it.
	msg := TaskMessage{Payload: "data"}
	m.Send(msg)
	got, ok := m.Receive(0)
	if !ok {
		t.Fatal("expected message with zero timeout")
	}
	if got.Payload != "data" {
		t.Fatalf("payload = %q, want %q", got.Payload, "data")
	}
}

func TestDrain(t *testing.T) {
	m := NewMailbox(8)
	for i := 0; i < 5; i++ {
		m.Send(TaskMessage{Payload: "msg"})
	}
	if m.Pending() != 5 {
		t.Fatalf("pending = %d, want 5", m.Pending())
	}
	count := m.Drain()
	if count != 5 {
		t.Fatalf("drained = %d, want 5", count)
	}
	if m.Pending() != 0 {
		t.Fatalf("pending after drain = %d, want 0", m.Pending())
	}
}

func TestDrainEmpty(t *testing.T) {
	m := NewMailbox(4)
	count := m.Drain()
	if count != 0 {
		t.Fatalf("drained = %d, want 0", count)
	}
}
