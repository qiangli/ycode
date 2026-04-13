package bus

import (
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// startTestNATS starts an embedded NATS server on a random port.
func startTestNATS(t *testing.T) (*natsserver.Server, *nats.Conn) {
	t.Helper()
	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1, // random port
		NoLog:  true,
		NoSigs: true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server failed to start")
	}
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)

	return srv, conn
}

func TestNATSBus_PublishSubscribe(t *testing.T) {
	_, conn := startTestNATS(t)
	b := NewNATSBus(conn)
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
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestNATSBus_FilteredSubscribe(t *testing.T) {
	_, conn := startTestNATS(t)
	b := NewNATSBus(conn)
	defer b.Close()

	ch, unsub := b.Subscribe(EventTextDelta)
	defer unsub()

	// Publish matching event.
	b.Publish(Event{Type: EventTextDelta, SessionID: "s1", Data: json.RawMessage(`{}`)})
	// Publish non-matching event.
	b.Publish(Event{Type: EventToolProgress, SessionID: "s1", Data: json.RawMessage(`{}`)})

	select {
	case ev := <-ch:
		if ev.Type != EventTextDelta {
			t.Errorf("got type %q, want %q", ev.Type, EventTextDelta)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Should not receive the non-matching event within a short window.
	select {
	case ev := <-ch:
		t.Errorf("should not receive non-matching event, got %q", ev.Type)
	case <-time.After(200 * time.Millisecond):
		// Expected — no more events.
	}
}

func TestNATSBus_InputChannel(t *testing.T) {
	_, conn := startTestNATS(t)
	b := NewNATSBus(conn)
	defer b.Close()

	ch, unsub := b.SubscribeInput()
	defer unsub()

	// Publish an input command.
	err := b.PublishInput("test-session", Event{
		Type: EventMessageSend,
		Data: json.RawMessage(`{"text":"hello from NATS"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-ch:
		if ev.Type != EventMessageSend {
			t.Errorf("got type %q, want %q", ev.Type, EventMessageSend)
		}
		if ev.SessionID != "test-session" {
			t.Errorf("got session %q, want %q", ev.SessionID, "test-session")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for input event")
	}
}

func TestNATSBus_Unsubscribe(t *testing.T) {
	_, conn := startTestNATS(t)
	b := NewNATSBus(conn)
	defer b.Close()

	ch, unsub := b.Subscribe()
	unsub()

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}
