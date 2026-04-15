package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsClient is a single WebSocket connection.
type wsClient struct {
	conn   *websocket.Conn
	roomID string
	send   chan []byte
}

// wsHub manages WebSocket clients and broadcasts messages.
type wsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{
		clients: make(map[*wsClient]struct{}),
	}
}

func (h *wsHub) run(ctx context.Context) {
	<-ctx.Done()
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		close(c.send)
		c.conn.Close()
	}
	h.clients = make(map[*wsClient]struct{})
}

func (h *wsHub) register(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *wsHub) unregister(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		close(c.send)
		delete(h.clients, c)
	}
}

// broadcast sends a message to all WebSocket clients subscribed to the message's room.
func (h *wsHub) broadcast(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.roomID != "" && c.roomID != msg.RoomID {
			continue
		}
		select {
		case c.send <- data:
		default:
			// Slow client — skip.
		}
	}
}

// StatusEvent is an ephemeral progress/status update sent to WebSocket clients.
// Unlike Messages, these are not persisted — they are live-only indicators.
type StatusEvent struct {
	Type   string `json:"type"` // "progress", "thinking", "tool", "error"
	RoomID string `json:"room_id"`
	Text   string `json:"text"`
}

// broadcastStatus sends an ephemeral status event to all WebSocket clients in a room.
func (h *wsHub) broadcastStatus(evt *StatusEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.roomID != "" && c.roomID != evt.RoomID {
			continue
		}
		select {
		case c.send <- data:
		default:
		}
	}
}

// serveWS upgrades an HTTP connection to WebSocket and registers the client.
func (h *wsHub) serveWS(w http.ResponseWriter, r *http.Request, roomID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("chat ws: upgrade failed", "error", err)
		return
	}

	client := &wsClient{
		conn:   conn,
		roomID: roomID,
		send:   make(chan []byte, 64),
	}
	h.register(client)

	// Writer goroutine.
	go func() {
		defer func() {
			h.unregister(client)
			conn.Close()
		}()
		for msg := range client.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Reader goroutine — keeps connection alive, reads close frames.
	go func() {
		defer func() {
			h.unregister(client)
			conn.Close()
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()
}
