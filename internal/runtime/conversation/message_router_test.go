package conversation

import (
	"testing"
	"time"
)

func TestMessageRouter_SendReceive(t *testing.T) {
	router := NewMessageRouter(10)
	ch := router.Register("agent-1")

	err := router.Send(AgentMessage{
		From:    "agent-0",
		To:      "agent-1",
		Type:    MessageTypeText,
		Content: "hello agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-ch:
		if msg.Content != "hello agent-1" {
			t.Errorf("expected 'hello agent-1', got %q", msg.Content)
		}
		if msg.SentAt.IsZero() {
			t.Error("SentAt should be set")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMessageRouter_SendUnknown(t *testing.T) {
	router := NewMessageRouter(10)
	err := router.Send(AgentMessage{To: "nonexistent"})
	if err == nil {
		t.Error("sending to unregistered agent should error")
	}
}

func TestMessageRouter_Broadcast(t *testing.T) {
	router := NewMessageRouter(10)
	ch1 := router.Register("agent-1")
	ch2 := router.Register("agent-2")
	_ = router.Register("sender")

	sent := router.Broadcast(AgentMessage{
		From:    "sender",
		Type:    MessageTypeText,
		Content: "broadcast msg",
	})
	if sent != 2 {
		t.Errorf("expected 2 sent, got %d", sent)
	}

	// Both agents should receive.
	select {
	case msg := <-ch1:
		if msg.Content != "broadcast msg" {
			t.Error("agent-1 wrong content")
		}
	case <-time.After(time.Second):
		t.Fatal("agent-1 timeout")
	}
	select {
	case msg := <-ch2:
		if msg.Content != "broadcast msg" {
			t.Error("agent-2 wrong content")
		}
	case <-time.After(time.Second):
		t.Fatal("agent-2 timeout")
	}
}

func TestMessageRouter_BroadcastSkipsSender(t *testing.T) {
	router := NewMessageRouter(10)
	ch := router.Register("sender")

	sent := router.Broadcast(AgentMessage{
		From:    "sender",
		Content: "self-broadcast",
	})
	if sent != 0 {
		t.Errorf("sender should not receive own broadcast, sent=%d", sent)
	}

	select {
	case <-ch:
		t.Error("sender should not receive own message")
	default:
		// OK.
	}
}

func TestMessageRouter_Unregister(t *testing.T) {
	router := NewMessageRouter(10)
	router.Register("agent-1")
	router.Unregister("agent-1")

	err := router.Send(AgentMessage{To: "agent-1"})
	if err == nil {
		t.Error("sending to unregistered agent should error")
	}
}

func TestMessageRouter_Drain(t *testing.T) {
	router := NewMessageRouter(10)
	router.Register("agent-1")

	for i := 0; i < 3; i++ {
		_ = router.Send(AgentMessage{To: "agent-1", Content: "msg"})
	}

	msgs := router.Drain("agent-1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 drained, got %d", len(msgs))
	}

	// Second drain should be empty.
	msgs = router.Drain("agent-1")
	if len(msgs) != 0 {
		t.Errorf("expected 0 after drain, got %d", len(msgs))
	}
}

func TestMessageRouter_RegisteredAgents(t *testing.T) {
	router := NewMessageRouter(10)
	router.Register("a")
	router.Register("b")

	agents := router.RegisteredAgents()
	if len(agents) != 2 {
		t.Errorf("expected 2, got %d", len(agents))
	}
}
