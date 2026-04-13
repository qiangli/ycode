package client

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
)

func startTestNATS(t *testing.T) (*natsserver.Server, string) {
	t.Helper()
	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1,
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
	return srv, srv.ClientURL()
}

func TestNATSClient_SendAndReceive(t *testing.T) {
	_, natsURL := startTestNATS(t)

	// Create server-side bus and service.
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}

	// Connect server-side NATS bus.
	serverConn, err := nats.Connect(natsURL, nats.Name("server"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(serverConn.Close)

	natsSrv := server.NewNATSServer(server.NATSConfig{
		Enabled: true,
		URL:     natsURL,
	}, svc)
	if err := natsSrv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { natsSrv.Stop() })

	// Create NATS client.
	natsClient, err := NewNATSClient(natsURL, "test-session")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { natsClient.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Subscribe to events.
	evCh, err := natsClient.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Send message via NATS client.
	err = natsClient.SendMessage(ctx, "test-session", service.MessageInput{Text: "hello via NATS"})
	if err != nil {
		t.Fatal(err)
	}

	// The server-side mock service will publish events.
	// Collect events from the client.
	var events []bus.Event
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-evCh:
			events = append(events, ev)
			if ev.Type == bus.EventTurnComplete {
				goto done
			}
		case <-timeout:
			// NATS pub/sub is fire-and-forget, the mock service publishes
			// to memBus which bridges to NATS. Check what we got.
			goto done
		}
	}
done:

	if len(events) < 3 {
		t.Logf("got %d events (expected 3: turn.start, text.delta, turn.complete)", len(events))
		for i, ev := range events {
			t.Logf("  event[%d]: type=%s data=%s", i, ev.Type, string(ev.Data))
		}
		t.Errorf("got %d events, want at least 3", len(events))
	}
}

func TestNATSClient_Events(t *testing.T) {
	_, natsURL := startTestNATS(t)

	// Create a NATS client and verify it receives published events.
	natsClient, err := NewNATSClient(natsURL, "test-session")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { natsClient.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	evCh, err := natsClient.Events(ctx, bus.EventTextDelta)
	if err != nil {
		t.Fatal(err)
	}

	// Publish directly to NATS (simulating server).
	conn, _ := nats.Connect(natsURL)
	defer conn.Close()

	ev := bus.Event{
		ID:        1,
		Type:      bus.EventTextDelta,
		SessionID: "test-session",
		Data:      json.RawMessage(`{"text":"direct publish"}`),
	}
	data, _ := json.Marshal(ev)
	conn.Publish(bus.EventSubject("test-session", bus.EventTextDelta), data)
	conn.Flush()

	select {
	case received := <-evCh:
		if received.Type != bus.EventTextDelta {
			t.Errorf("got type %q, want %q", received.Type, bus.EventTextDelta)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}
