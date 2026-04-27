package swarm

import (
	"testing"
	"time"
)

func TestMailbox_SendAndReceive(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	msg := MailboxMessage{
		From:    "agent-a",
		Type:    "task",
		Content: "do something",
	}
	if err := mb.Send(msg); err != nil {
		t.Fatal(err)
	}

	received, err := mb.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if received == nil {
		t.Fatal("expected message, got nil")
	}
	if received.From != "agent-a" {
		t.Errorf("expected from agent-a, got %s", received.From)
	}
	if received.Content != "do something" {
		t.Errorf("expected 'do something', got %s", received.Content)
	}
	if received.ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestMailbox_Peek(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	msg := MailboxMessage{
		From:    "agent-b",
		Type:    "ping",
		Content: "hello",
	}
	if err := mb.Send(msg); err != nil {
		t.Fatal(err)
	}

	peeked, err := mb.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if peeked == nil {
		t.Fatal("expected message from peek")
	}

	// Message should still be there.
	if mb.Count() != 1 {
		t.Errorf("expected count 1 after peek, got %d", mb.Count())
	}
}

func TestMailbox_Count(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	if mb.Count() != 0 {
		t.Errorf("expected 0, got %d", mb.Count())
	}

	for i := 0; i < 3; i++ {
		if err := mb.Send(MailboxMessage{Type: "task", Content: "msg"}); err != nil {
			t.Fatal(err)
		}
		// Small delay to ensure different mod times for ordering.
		time.Sleep(10 * time.Millisecond)
	}

	if mb.Count() != 3 {
		t.Errorf("expected 3, got %d", mb.Count())
	}
}

func TestMailbox_EmptyReceive(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := mb.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if msg != nil {
		t.Error("expected nil from empty mailbox")
	}
}

func TestMailbox_EmptyPeek(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := mb.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if msg != nil {
		t.Error("expected nil from empty mailbox peek")
	}
}

func TestMailbox_FIFO(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewMailbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		msg := MailboxMessage{
			From:    "sender",
			Type:    "task",
			Content: string(rune('a' + i)),
		}
		if err := mb.Send(msg); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Should receive in order.
	first, err := mb.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != "a" {
		t.Errorf("expected first message 'a', got %s", first.Content)
	}
}
