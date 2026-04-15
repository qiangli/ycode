package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/chat/channel"
)

const (
	natsRoomMessages = "ycode.chat.rooms.%s.messages"
	natsRoomPresence = "ycode.chat.rooms.%s.presence"
	natsChanStatus   = "ycode.chat.channels.%s.status"
)

// HubConfig holds the configuration for the chat hub.
type HubConfig struct {
	Enabled  bool                                `json:"enabled"`
	Channels map[channel.ChannelID]ChannelConfig `json:"channels,omitempty"`
}

// ChannelConfig configures a single channel.
type ChannelConfig struct {
	Enabled  bool                    `json:"enabled"`
	Accounts []channel.AccountConfig `json:"accounts,omitempty"`
}

// Hub is the central messaging component. It implements observability.Component
// and manages channels, routing, persistence, and the web UI.
type Hub struct {
	conn     *nats.Conn
	config   *HubConfig
	store    *Store
	router   *Router
	logger   *slog.Logger
	healthy  atomic.Bool
	handler  http.Handler
	dataDir  string

	mu       sync.RWMutex
	channels map[channel.ChannelID]channel.Channel
	inbound  chan channel.InboundMessage
	cancel   context.CancelFunc

	// WebSocket clients for the web channel.
	wsClients *wsHub
}

// NewHub creates a new chat hub.
func NewHub(conn *nats.Conn, config *HubConfig, dataDir string) *Hub {
	h := &Hub{
		conn:     conn,
		config:   config,
		dataDir:  dataDir,
		logger:   slog.Default(),
		channels: make(map[channel.ChannelID]channel.Channel),
		inbound:  make(chan channel.InboundMessage, 256),
	}
	h.wsClients = newWSHub()
	h.handler = h.buildHTTPHandler()
	return h
}

// Name implements observability.Component.
func (h *Hub) Name() string { return "chat" }

// Start implements observability.Component.
func (h *Hub) Start(ctx context.Context) error {
	store, err := NewStore(h.dataDir)
	if err != nil {
		return fmt.Errorf("chat hub: %w", err)
	}
	h.store = store
	h.router = NewRouter(store)

	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel

	// Start the inbound message processing loop.
	go h.processInbound(ctx)

	// Start the WebSocket hub.
	go h.wsClients.run(ctx)

	// Register and start channel adapters.
	if err := h.startChannels(ctx); err != nil {
		h.logger.Warn("chat hub: some channels failed to start", "error", err)
	}

	h.healthy.Store(true)
	h.logger.Info("chat hub: started", "data", h.dataDir)
	return nil
}

// Stop implements observability.Component.
func (h *Hub) Stop(ctx context.Context) error {
	h.healthy.Store(false)
	if h.cancel != nil {
		h.cancel()
	}

	h.mu.RLock()
	channels := make([]channel.Channel, 0, len(h.channels))
	for _, ch := range h.channels {
		channels = append(channels, ch)
	}
	h.mu.RUnlock()

	for _, ch := range channels {
		if err := ch.Stop(ctx); err != nil {
			h.logger.Warn("chat hub: channel stop error", "channel", ch.ID(), "error", err)
		}
	}

	if h.store != nil {
		h.store.Close()
	}
	return nil
}

// Healthy implements observability.Component.
func (h *Hub) Healthy() bool { return h.healthy.Load() }

// HTTPHandler implements observability.Component.
func (h *Hub) HTTPHandler() http.Handler { return h.handler }

// RegisterChannel adds a channel adapter to the hub.
func (h *Hub) RegisterChannel(ch channel.Channel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.channels[ch.ID()] = ch
}

// startChannels starts all configured and registered channel adapters.
func (h *Hub) startChannels(ctx context.Context) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var firstErr error
	for id, ch := range h.channels {
		cfg, ok := h.config.Channels[id]
		if !ok || !cfg.Enabled {
			h.logger.Info("chat hub: channel disabled", "channel", id)
			continue
		}
		if err := ch.Start(ctx, cfg.Accounts, h.inbound); err != nil {
			h.logger.Error("chat hub: channel start failed", "channel", id, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		h.logger.Info("chat hub: channel started", "channel", id)
	}
	return firstErr
}

// processInbound reads inbound messages from all channels, routes them,
// persists them, publishes to NATS, and fans out to other channels.
func (h *Hub) processInbound(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-h.inbound:
			h.handleInbound(ctx, in)
		}
	}
}

func (h *Hub) handleInbound(ctx context.Context, in channel.InboundMessage) {
	// Resolve the room.
	room, err := h.router.ResolveRoom(in.ChannelID, in.AccountID, in.ChatID)
	if err != nil {
		h.logger.Error("chat hub: resolve room", "error", err)
		return
	}

	// Find or create the user.
	user, err := h.store.FindOrCreateUser(in.ChannelID, in.SenderID, in.SenderName)
	if err != nil {
		h.logger.Error("chat hub: find/create user", "error", err)
		return
	}

	// Build the hub message.
	msg := &Message{
		ID:     uuid.New().String(),
		RoomID: room.ID,
		Sender: Sender{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			ChannelID:   in.ChannelID,
			PlatformID:  in.SenderID,
		},
		Timestamp: in.Timestamp,
		Content:   in.Content,
		ThreadID:  in.ThreadID,
		Origin: MessageOrigin{
			ChannelID:  in.ChannelID,
			AccountID:  in.AccountID,
			PlatformID: in.PlatformID,
		},
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Persist.
	if err := h.store.SaveMessage(msg); err != nil {
		h.logger.Error("chat hub: save message", "error", err)
	}

	// Publish to NATS for subscribers (web UI, etc.).
	h.publishMessage(msg)

	// Broadcast to WebSocket clients.
	h.wsClients.broadcast(msg)

	// Fan out to other channel bindings.
	targets, err := h.router.FanOutTargets(room.ID, in.ChannelID, in.AccountID, in.ChatID)
	if err != nil {
		h.logger.Error("chat hub: fan out targets", "error", err)
		return
	}
	for _, t := range targets {
		h.mu.RLock()
		ch, ok := h.channels[t.ChannelID]
		h.mu.RUnlock()
		if !ok {
			continue
		}
		outMsg := channel.OutboundMessage{Content: msg.Content}
		target := channel.OutboundTarget{
			ChatID:    t.ChatID,
			AccountID: t.AccountID,
		}
		if err := ch.Send(ctx, target, outMsg); err != nil {
			h.logger.Error("chat hub: fan out send", "channel", t.ChannelID, "error", err)
		}
	}
}

func (h *Hub) publishMessage(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	subject := fmt.Sprintf(natsRoomMessages, msg.RoomID)
	h.conn.Publish(subject, data)
}

// buildHTTPHandler constructs the mux for REST API and static web UI.
func (h *Hub) buildHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// REST API.
	mux.HandleFunc("GET /api/health", h.handleHealth)
	mux.HandleFunc("GET /api/rooms", h.handleListRooms)
	mux.HandleFunc("POST /api/rooms", h.handleCreateRoom)
	mux.HandleFunc("GET /api/rooms/{id}/messages", h.handleGetMessages)
	mux.HandleFunc("POST /api/rooms/{id}/messages", h.handleSendMessage)
	mux.HandleFunc("GET /api/rooms/{id}/ws", h.handleWebSocket)
	mux.HandleFunc("GET /api/channels", h.handleListChannels)

	// Static web UI — serve embedded files, SPA fallback.
	staticHandler := chatWebHandler()
	mux.Handle("/", staticHandler)

	return mux
}

func (h *Hub) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"healthy": h.healthy.Load()})
}

func (h *Hub) handleListRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.store.ListRooms()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rooms == nil {
		rooms = []*Room{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rooms)
}

func (h *Hub) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "New Room"
	}
	room, err := h.store.CreateRoom(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Add a web binding so the room is accessible from the web UI.
	h.store.AddBinding(room.ID, channel.ChannelWeb, "default", room.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(room)
}

func (h *Hub) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	messages, err := h.store.GetMessages(roomID, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if messages == nil {
		messages = []*Message{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

func (h *Hub) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	var req struct {
		Text       string `json:"text"`
		SenderName string `json:"sender_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	user, err := h.store.FindOrCreateUser(channel.ChannelWeb, "web-user", req.SenderName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.SenderName != "" && user.DisplayName != req.SenderName {
		user.DisplayName = req.SenderName
	}

	// Push through the inbound pipeline so routing and fan-out happen.
	h.inbound <- channel.InboundMessage{
		ChannelID:  channel.ChannelWeb,
		AccountID:  "default",
		SenderID:   user.PlatformID,
		SenderName: user.DisplayName,
		Content:    channel.MessageContent{Text: req.Text},
		PlatformID: uuid.New().String(),
		ChatID:     roomID,
		Timestamp:  time.Now(),
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Hub) handleListChannels(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	type channelStatus struct {
		ID           channel.ChannelID       `json:"id"`
		Healthy      bool                    `json:"healthy"`
		Capabilities channel.Capabilities    `json:"capabilities"`
	}
	var statuses []channelStatus
	for _, ch := range h.channels {
		statuses = append(statuses, channelStatus{
			ID:           ch.ID(),
			Healthy:      ch.Healthy(),
			Capabilities: ch.Capabilities(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statuses)
}

func (h *Hub) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	h.wsClients.serveWS(w, r, roomID)
}
