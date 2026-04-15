package adapters

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/chat/channel"
	"github.com/qiangli/ycode/internal/service"
)

// AgentChannel bridges chat messages to the AI agent service.
// It implements channel.Channel — the AI is just another "platform" in the hub.
//
// When the hub fans out a message to this channel, Send() dispatches it to
// service.SendMessage(). When the AI finishes a turn, the bus event is
// converted back to an InboundMessage and pushed into the hub.
type AgentChannel struct {
	svc     service.Service
	logger  *slog.Logger
	healthy atomic.Bool
	inbound chan<- channel.InboundMessage
	cancel  context.CancelFunc

	// Bidirectional room ↔ session mapping.
	roomToSession sync.Map // roomID -> sessionID
	sessionToRoom sync.Map // sessionID -> roomID

	mu sync.Mutex // guards session creation
}

// NewAgentChannel creates an agent channel adapter wrapping the given service.
func NewAgentChannel(svc service.Service) *AgentChannel {
	return &AgentChannel{
		svc:    svc,
		logger: slog.Default(),
	}
}

func (a *AgentChannel) ID() channel.ChannelID { return channel.ChannelAgent }

func (a *AgentChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:  false,
		Media:    false,
		Markdown: true,
	}
}

func (a *AgentChannel) Start(ctx context.Context, _ []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	a.inbound = inbound
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Subscribe to turn completion and error events from the AI service bus.
	ch, unsub := a.svc.Bus().Subscribe(bus.EventTurnComplete, bus.EventTurnError)
	go a.listenEvents(ctx, ch, unsub)

	a.healthy.Store(true)
	a.logger.Info("agent: channel started")
	return nil
}

func (a *AgentChannel) Stop(_ context.Context) error {
	a.healthy.Store(false)
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *AgentChannel) Healthy() bool { return a.healthy.Load() }

// Send is called by the hub when a message should reach the AI agent.
// It dispatches to service.SendMessage in a goroutine (non-blocking).
func (a *AgentChannel) Send(_ context.Context, target channel.OutboundTarget, msg channel.OutboundMessage) error {
	roomID := target.ChatID
	sessionID, err := a.getOrCreateSession(roomID)
	if err != nil {
		return err
	}

	go func() {
		if err := a.svc.SendMessage(context.Background(), sessionID, bus.MessageInput{
			Text: msg.Content.Text,
		}); err != nil {
			a.logger.Error("agent: SendMessage failed", "room", roomID, "error", err)
			// Post error as a chat message so the user sees it.
			a.postToHub(roomID, "Error: "+err.Error())
		}
	}()
	return nil
}

// getOrCreateSession returns the AI session for a room, creating one if needed.
func (a *AgentChannel) getOrCreateSession(roomID string) (string, error) {
	if v, ok := a.roomToSession.Load(roomID); ok {
		return v.(string), nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring lock.
	if v, ok := a.roomToSession.Load(roomID); ok {
		return v.(string), nil
	}

	info, err := a.svc.CreateSession(context.Background())
	if err != nil {
		return "", err
	}

	a.roomToSession.Store(roomID, info.ID)
	a.sessionToRoom.Store(info.ID, roomID)
	a.logger.Info("agent: created session", "room", roomID, "session", info.ID)
	return info.ID, nil
}

// listenEvents reads bus events and converts AI responses to chat messages.
func (a *AgentChannel) listenEvents(ctx context.Context, ch <-chan bus.Event, unsub func()) {
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			a.handleEvent(ev)
		}
	}
}

func (a *AgentChannel) handleEvent(ev bus.Event) {
	// Look up room from session.
	roomID, ok := a.sessionToRoom.Load(ev.SessionID)
	if !ok {
		return // Not a session we manage.
	}

	switch ev.Type {
	case bus.EventTurnComplete:
		var data struct {
			Status string `json:"status"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			return
		}
		if data.Text == "" {
			return
		}
		a.postToHub(roomID.(string), data.Text)

	case bus.EventTurnError:
		var data struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			return
		}
		if data.Error != "" {
			a.postToHub(roomID.(string), "Error: "+data.Error)
		}
	}
}

// postToHub pushes a message from the AI agent into the chat hub.
func (a *AgentChannel) postToHub(roomID, text string) {
	if a.inbound == nil {
		return
	}
	a.inbound <- channel.InboundMessage{
		ChannelID:  channel.ChannelAgent,
		AccountID:  "default",
		SenderID:   "ai-agent",
		SenderName: "AI Assistant",
		Content:    channel.MessageContent{Text: text},
		PlatformID: uuid.New().String(),
		ChatID:     roomID,
		Timestamp:  time.Now(),
	}
}
