//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocket(t *testing.T) {
	requireConnectivity(t)

	wsBaseURL := func() string {
		base := baseURL(t)
		return "ws" + strings.TrimPrefix(base, "http") + "/ycode"
	}

	t.Run("ConnectAndReceivePing", func(t *testing.T) {
		url := wsBaseURL() + "/api/sessions/ws-test-connect/ws"
		dialer := websocket.Dialer{
			HandshakeTimeout: 5 * time.Second,
		}
		conn, resp, err := dialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("dial %s: %v", url, err)
		}
		defer conn.Close()
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Errorf("handshake status %d, want 101", resp.StatusCode)
		}

		// Set a pong handler to verify keepalive pings arrive.
		gotPong := make(chan struct{}, 1)
		conn.SetPingHandler(func(msg string) error {
			select {
			case gotPong <- struct{}{}:
			default:
			}
			return conn.WriteControl(websocket.PongMessage, []byte(msg), time.Now().Add(time.Second))
		})

		// Read messages in background; we mainly care about the connection being alive.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
			}
		}()

		// Wait for a ping (server sends keepalive pings every 10s).
		select {
		case <-gotPong:
			// Ping received — connection is alive.
		case <-time.After(15 * time.Second):
			t.Error("no ping received within 15s")
		case <-done:
			t.Error("connection closed before ping")
		}
	})

	t.Run("SendMessageFormat", func(t *testing.T) {
		url := wsBaseURL() + "/api/sessions/ws-test-send/ws"
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		// Send a well-formed message.
		msg := map[string]any{
			"type": "message.send",
			"data": map[string]any{
				"text":  "hello from integration test",
				"files": []string{},
			},
		}
		if err := conn.WriteJSON(msg); err != nil {
			t.Fatalf("write message: %v", err)
		}

		// Read at least one event back (could be turn.start, text.delta, or turn.error).
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read response: %v", err)
		}

		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatalf("unmarshal event: %v; raw: %s", err, string(raw))
		}
		if event.Type == "" {
			t.Errorf("event type is empty; raw: %s", string(raw))
		}
	})

	t.Run("ConcurrentSessions", func(t *testing.T) {
		const n = 3
		errs := make(chan error, n)

		for i := range n {
			go func() {
				url := wsBaseURL() + fmt.Sprintf("/api/sessions/ws-concurrent-%d/ws", i)
				conn, _, err := websocket.DefaultDialer.Dial(url, nil)
				if err != nil {
					errs <- fmt.Errorf("session %d dial: %w", i, err)
					return
				}
				defer conn.Close()

				// Verify the connection is usable by writing and reading.
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					errs <- fmt.Errorf("session %d ping: %w", i, err)
					return
				}
				errs <- nil
			}()
		}

		for range n {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
	})

	t.Run("Reconnect", func(t *testing.T) {
		sessionID := "ws-test-reconnect"
		url := wsBaseURL() + "/api/sessions/" + sessionID + "/ws"

		// First connection.
		conn1, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("first dial: %v", err)
		}
		conn1.Close()

		// Brief pause then reconnect to same session.
		time.Sleep(100 * time.Millisecond)

		conn2, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("reconnect dial: %v", err)
		}
		defer conn2.Close()

		// Verify reconnected connection is usable.
		if err := conn2.WriteMessage(websocket.PingMessage, nil); err != nil {
			t.Errorf("reconnect ping: %v", err)
		}
	})
}
