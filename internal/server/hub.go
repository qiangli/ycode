package server

import (
	"log/slog"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// ClientID is a UUID assigned per TUI/web/external client, persistent across reconnects.
type ClientID string

// ClientKind identifies the transport type.
type ClientKind string

const (
	ClientTUI      ClientKind = "tui"
	ClientWeb      ClientKind = "web"
	ClientSlack    ClientKind = "slack"
	ClientDiscord  ClientKind = "discord"
	ClientTelegram ClientKind = "telegram"
)

// Client represents a connected client with its outbound event queue.
type Client struct {
	ID        ClientID
	Kind      ClientKind
	SessionID string
	WorkDir   string
	Send      chan bus.Event // buffered outbound queue
	JoinedAt  time.Time
}

// Hub manages client connections and routes events to the correct recipients.
// It follows the gorilla/websocket hub pattern with session-aware fan-out.
type Hub struct {
	// clients tracks all connected clients by ID.
	clients map[ClientID]*Client

	// sessions indexes clients by sessionID for targeted delivery.
	sessions map[string]map[ClientID]struct{}

	// groups indexes sessionIDs by groupID for group delivery (future).
	groups map[string]map[string]struct{} // groupID → set of sessionIDs

	register   chan *Client
	unregister chan ClientID
	dispatch   chan bus.Event // events routed by SessionID/GroupID
	broadcast  chan bus.Event // events sent to all clients

	mu   sync.RWMutex
	done chan struct{}
}

// NewHub creates a new Hub. Call Run() to start the event loop.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[ClientID]*Client),
		sessions:   make(map[string]map[ClientID]struct{}),
		groups:     make(map[string]map[string]struct{}),
		register:   make(chan *Client, 16),
		unregister: make(chan ClientID, 16),
		dispatch:   make(chan bus.Event, 256),
		broadcast:  make(chan bus.Event, 64),
		done:       make(chan struct{}),
	}
}

// Run starts the hub event loop. It blocks until Stop is called.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			if client.SessionID != "" {
				if h.sessions[client.SessionID] == nil {
					h.sessions[client.SessionID] = make(map[ClientID]struct{})
				}
				h.sessions[client.SessionID][client.ID] = struct{}{}
			}
			h.mu.Unlock()
			slog.Debug("hub: client registered",
				"client_id", client.ID,
				"kind", client.Kind,
				"session_id", client.SessionID,
				"work_dir", client.WorkDir,
			)

		case clientID := <-h.unregister:
			h.mu.Lock()
			if client, ok := h.clients[clientID]; ok {
				if client.SessionID != "" {
					if set, ok := h.sessions[client.SessionID]; ok {
						delete(set, clientID)
						if len(set) == 0 {
							delete(h.sessions, client.SessionID)
						}
					}
				}
				close(client.Send)
				delete(h.clients, clientID)
			}
			h.mu.Unlock()
			slog.Debug("hub: client unregistered", "client_id", clientID)

		case ev := <-h.dispatch:
			h.routeEvent(ev)

		case ev := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.Send <- ev:
				default:
					// Slow client — drop event.
				}
			}
			h.mu.RUnlock()

		case <-h.done:
			h.mu.Lock()
			for _, client := range h.clients {
				close(client.Send)
			}
			h.clients = make(map[ClientID]*Client)
			h.sessions = make(map[string]map[ClientID]struct{})
			h.mu.Unlock()
			return
		}
	}
}

// Stop shuts down the hub event loop.
func (h *Hub) Stop() {
	close(h.done)
}

// Register adds a client to the hub.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(clientID ClientID) {
	h.unregister <- clientID
}

// Dispatch routes an event based on its SessionID/GroupID.
// Events with empty SessionID are broadcast to all clients.
func (h *Hub) Dispatch(ev bus.Event) {
	if ev.SessionID == "" && ev.GroupID == "" {
		h.broadcast <- ev
		return
	}
	h.dispatch <- ev
}

// Broadcast sends an event to all connected clients.
func (h *Hub) Broadcast(ev bus.Event) {
	h.broadcast <- ev
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// SessionClients returns the client IDs attached to a session.
func (h *Hub) SessionClients(sessionID string) []ClientID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	set := h.sessions[sessionID]
	ids := make([]ClientID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}

// routeEvent delivers an event to the correct set of clients.
func (h *Hub) routeEvent(ev bus.Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Group delivery: send to all clients in all sessions belonging to the group.
	if ev.GroupID != "" {
		if sessionIDs, ok := h.groups[ev.GroupID]; ok {
			for sessionID := range sessionIDs {
				h.sendToSession(sessionID, ev)
			}
		}
		return
	}

	// Session delivery: send to all clients in the session.
	if ev.SessionID != "" {
		h.sendToSession(ev.SessionID, ev)
	}
}

// sendToSession delivers an event to all clients in a session. Caller must hold mu.RLock.
func (h *Hub) sendToSession(sessionID string, ev bus.Event) {
	clientIDs, ok := h.sessions[sessionID]
	if !ok {
		return
	}
	for clientID := range clientIDs {
		if client, ok := h.clients[clientID]; ok {
			select {
			case client.Send <- ev:
			default:
				// Slow client — drop event.
			}
		}
	}
}

// AddToGroup adds a session to a group (for future team agent support).
func (h *Hub) AddToGroup(groupID, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.groups[groupID] == nil {
		h.groups[groupID] = make(map[string]struct{})
	}
	h.groups[groupID][sessionID] = struct{}{}
}

// RemoveFromGroup removes a session from a group.
func (h *Hub) RemoveFromGroup(groupID, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.groups[groupID]; ok {
		delete(set, sessionID)
		if len(set) == 0 {
			delete(h.groups, groupID)
		}
	}
}
