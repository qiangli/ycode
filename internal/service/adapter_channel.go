package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/chat/channel"
)

// ChannelAdapter bridges an existing chat channel.Channel implementation
// into the ExternalAdapter interface for coding sessions.
//
// This allows Slack, Discord, Telegram, etc. adapters that were originally
// built for the chat hub to also serve as coding session clients.
type ChannelAdapter struct {
	ch       channel.Channel
	accounts []channel.AccountConfig
	inbound  chan channel.InboundMessage
	handler  InboundHandler
}

// NewChannelAdapter wraps a channel.Channel as an ExternalAdapter.
func NewChannelAdapter(ch channel.Channel, accounts []channel.AccountConfig) *ChannelAdapter {
	return &ChannelAdapter{
		ch:       ch,
		accounts: accounts,
		inbound:  make(chan channel.InboundMessage, 256),
	}
}

func (a *ChannelAdapter) ID() string {
	return string(a.ch.ID())
}

func (a *ChannelAdapter) Start(ctx context.Context, handler InboundHandler) error {
	a.handler = handler

	// Start the underlying channel — it pushes messages onto a.inbound.
	if err := a.ch.Start(ctx, a.accounts, a.inbound); err != nil {
		return err
	}

	// Pump inbound messages to the handler.
	go a.inboundPump(ctx)
	return nil
}

func (a *ChannelAdapter) Stop(ctx context.Context) error {
	return a.ch.Stop(ctx)
}

// Send delivers a ycode event back to the external platform.
func (a *ChannelAdapter) Send(ctx context.Context, externalRef string, event bus.Event) error {
	// Extract text from the event data.
	var data map[string]any
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return nil // skip non-JSON events
	}

	var text string
	switch event.Type {
	case bus.EventTextDelta:
		if t, ok := data["text"].(string); ok {
			text = t
		}
	case bus.EventTurnComplete:
		if t, ok := data["text"].(string); ok {
			text = t
		}
	case bus.EventCommandComplete:
		if t, ok := data["result"].(string); ok {
			text = t
		}
	case bus.EventCommandProgress:
		if t, ok := data["message"].(string); ok {
			text = t
		}
	default:
		return nil // skip events we don't translate
	}

	if text == "" {
		return nil
	}

	// Deliver to the platform. Use externalRef as the chat ID.
	target := channel.OutboundTarget{
		ChatID: externalRef,
	}
	return a.ch.Send(ctx, target, channel.OutboundMessage{
		Content: channel.MessageContent{Text: text},
	})
}

// inboundPump reads from the channel's inbound queue and forwards to the handler.
func (a *ChannelAdapter) inboundPump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-a.inbound:
			if !ok {
				return
			}
			// Build external ref from channel + chat ID.
			externalRef := fmt.Sprintf("%s:%s", msg.ChannelID, msg.ChatID)
			if err := a.handler.OnMessage(ctx, externalRef, string(msg.ChannelID), msg.Content.Text); err != nil {
				slog.Warn("adapter: inbound message failed",
					"channel", msg.ChannelID,
					"chat_id", msg.ChatID,
					"error", err,
				)
			}
		}
	}
}
