package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/chat/channel"
	"github.com/qiangli/ycode/internal/service"
)

// StatusBroadcaster is the interface the agent adapter uses to send
// ephemeral progress events to WebSocket clients. Implemented by Hub.
type StatusBroadcaster interface {
	BroadcastStatus(roomID, statusType, text string)
}

// AgentChannel bridges chat messages to the AI agent service.
// It implements channel.Channel — the AI is just another "platform" in the hub.
type AgentChannel struct {
	svc     service.Service
	status  StatusBroadcaster
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
// The StatusBroadcaster (typically the Hub) is used to relay progress events.
func NewAgentChannel(svc service.Service, status StatusBroadcaster) *AgentChannel {
	return &AgentChannel{
		svc:    svc,
		status: status,
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

	// Subscribe to all relevant events from the AI service bus.
	ch, unsub := a.svc.Bus().Subscribe(
		bus.EventTurnStart,
		bus.EventTextDelta,
		bus.EventThinkingDelta,
		bus.EventToolProgress,
		bus.EventToolResult,
		bus.EventTurnComplete,
		bus.EventTurnError,
	)
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
			a.postToHub(roomID, "Error: "+err.Error())
		}
	}()
	return nil
}

func (a *AgentChannel) getOrCreateSession(roomID string) (string, error) {
	if v, ok := a.roomToSession.Load(roomID); ok {
		return v.(string), nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

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
	roomVal, ok := a.sessionToRoom.Load(ev.SessionID)
	if !ok {
		return
	}
	roomID := roomVal.(string)

	switch ev.Type {
	case bus.EventTurnStart:
		a.broadcastProgress(roomID, "thinking", "AI is thinking...")

	case bus.EventTextDelta:
		// Streaming text — show a brief "generating" status.
		a.broadcastProgress(roomID, "thinking", "AI is generating a response...")

	case bus.EventThinkingDelta:
		a.broadcastProgress(roomID, "thinking", "AI is reasoning...")

	case bus.EventToolProgress:
		var data struct {
			Tool   string `json:"tool"`
			Status string `json:"status"`
			Index  int    `json:"index"`
			Total  int    `json:"total"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			text := fmt.Sprintf("Running tool: %s (%d/%d) [%s]", data.Tool, data.Index+1, data.Total, data.Status)
			a.broadcastProgress(roomID, "tool", text)
		}

	case bus.EventToolResult:
		var data struct {
			Status  string `json:"status"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			if data.IsError {
				a.broadcastProgress(roomID, "tool", "Tool failed")
			} else {
				a.broadcastProgress(roomID, "tool", "Tool completed")
			}
		}

	case bus.EventTurnComplete:
		// Clear progress, post final response as a chat message.
		a.broadcastProgress(roomID, "done", "")
		var data struct {
			Status string `json:"status"`
			Text   string `json:"text"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.Text != "" {
			a.postToHub(roomID, data.Text)
		}

	case bus.EventTurnError:
		a.broadcastProgress(roomID, "done", "")
		var data struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.Error != "" {
			a.postToHub(roomID, "Error: "+data.Error)
		}
	}
}

func (a *AgentChannel) broadcastProgress(roomID, statusType, text string) {
	if a.status != nil {
		a.status.BroadcastStatus(roomID, statusType, text)
	}
}

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
