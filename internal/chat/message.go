// Package chat implements a NATS-based messaging hub with platform bridges.
package chat

import (
	"time"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// Message is a hub-internal message stored in the database and delivered via NATS.
type Message struct {
	ID        string                 `json:"id"`
	RoomID    string                 `json:"room_id"`
	Sender    Sender                 `json:"sender"`
	Timestamp time.Time              `json:"timestamp"`
	Content   channel.MessageContent `json:"content"`
	ReplyTo   string                 `json:"reply_to,omitempty"`
	ThreadID  string                 `json:"thread_id,omitempty"`
	Origin    MessageOrigin          `json:"origin"`
}

// Sender identifies who sent a message.
type Sender struct {
	ID          string            `json:"id"`           // hub-internal user ID
	DisplayName string            `json:"display_name"`
	ChannelID   channel.ChannelID `json:"channel_id"`
	PlatformID  string            `json:"platform_id"` // platform-native user ID
}

// MessageOrigin tracks where a message originally came from.
type MessageOrigin struct {
	ChannelID  channel.ChannelID `json:"channel_id"`
	AccountID  string            `json:"account_id"`
	PlatformID string            `json:"platform_id"` // platform-native message ID
}
