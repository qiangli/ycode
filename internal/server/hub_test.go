package server

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func newTestClient(id string, sessionID string) *Client {
	return &Client{
		ID:        ClientID(id),
		Kind:      ClientTUI,
		SessionID: sessionID,
		Send:      make(chan bus.Event, 64),
		JoinedAt:  time.Now(),
	}
}

func TestHub_RegisterUnregister(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	c := newTestClient("c1", "s1")
	h.Register(c)

	// Give the hub goroutine time to process.
	time.Sleep(10 * time.Millisecond)

	if h.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", h.ClientCount())
	}

	clients := h.SessionClients("s1")
	if len(clients) != 1 || clients[0] != "c1" {
		t.Errorf("expected [c1] in session s1, got %v", clients)
	}

	h.Unregister("c1")
	time.Sleep(10 * time.Millisecond)

	if h.ClientCount() != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", h.ClientCount())
	}
}

func TestHub_SessionIsolation(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	c1 := newTestClient("c1", "session-A")
	c2 := newTestClient("c2", "session-B")
	h.Register(c1)
	h.Register(c2)
	time.Sleep(10 * time.Millisecond)

	// Dispatch event for session-A only.
	h.Dispatch(bus.Event{
		Type:      bus.EventTurnStart,
		SessionID: "session-A",
	})
	time.Sleep(10 * time.Millisecond)

	// c1 should receive it.
	select {
	case ev := <-c1.Send:
		if ev.SessionID != "session-A" {
			t.Errorf("c1 got wrong session: %s", ev.SessionID)
		}
	default:
		t.Error("c1 should have received the event")
	}

	// c2 should NOT receive it.
	select {
	case ev := <-c2.Send:
		t.Errorf("c2 should not receive session-A event, got: %v", ev)
	default:
		// expected
	}
}

func TestHub_Broadcast(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	c1 := newTestClient("c1", "s1")
	c2 := newTestClient("c2", "s2")
	h.Register(c1)
	h.Register(c2)
	time.Sleep(10 * time.Millisecond)

	// Broadcast (empty SessionID).
	h.Broadcast(bus.Event{
		Type: bus.EventType("server.announce"),
	})
	time.Sleep(10 * time.Millisecond)

	// Both should receive it.
	for _, c := range []*Client{c1, c2} {
		select {
		case <-c.Send:
			// expected
		default:
			t.Errorf("client %s should have received broadcast", c.ID)
		}
	}
}

func TestHub_GroupDelivery(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	c1 := newTestClient("c1", "s1")
	c2 := newTestClient("c2", "s2")
	c3 := newTestClient("c3", "s3")
	h.Register(c1)
	h.Register(c2)
	h.Register(c3)
	time.Sleep(10 * time.Millisecond)

	// Add s1 and s2 to a group, leave s3 out.
	h.AddToGroup("team-alpha", "s1")
	h.AddToGroup("team-alpha", "s2")

	// Dispatch group event.
	h.Dispatch(bus.Event{
		Type:    bus.EventType("team.update"),
		GroupID: "team-alpha",
	})
	time.Sleep(10 * time.Millisecond)

	// c1 and c2 should receive.
	for _, c := range []*Client{c1, c2} {
		select {
		case <-c.Send:
			// expected
		default:
			t.Errorf("client %s should have received group event", c.ID)
		}
	}

	// c3 should NOT.
	select {
	case <-c3.Send:
		t.Error("c3 should not receive team-alpha event")
	default:
		// expected
	}
}

func TestHub_MultipleClientsPerSession(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	// Two clients in the same session (e.g., TUI + web).
	c1 := newTestClient("c1", "shared-session")
	c2 := newTestClient("c2", "shared-session")
	h.Register(c1)
	h.Register(c2)
	time.Sleep(10 * time.Millisecond)

	clients := h.SessionClients("shared-session")
	if len(clients) != 2 {
		t.Errorf("expected 2 clients in session, got %d", len(clients))
	}

	// Both should receive session event.
	h.Dispatch(bus.Event{
		Type:      bus.EventTextDelta,
		SessionID: "shared-session",
	})
	time.Sleep(10 * time.Millisecond)

	for _, c := range []*Client{c1, c2} {
		select {
		case <-c.Send:
		default:
			t.Errorf("client %s should have received session event", c.ID)
		}
	}
}

func TestHub_UnregisterCleansSession(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	c := newTestClient("c1", "s1")
	h.Register(c)
	time.Sleep(10 * time.Millisecond)

	h.Unregister("c1")
	time.Sleep(10 * time.Millisecond)

	clients := h.SessionClients("s1")
	if len(clients) != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", len(clients))
	}
}
