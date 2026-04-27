// Package channel defines the adapter interface for messaging platform channels.
package channel

import (
	"context"
	"time"
)

// ChannelID identifies a messaging platform.
type ChannelID string

const (
	ChannelWeb      ChannelID = "web"
	ChannelTelegram ChannelID = "telegram"
	ChannelDiscord  ChannelID = "discord"
	ChannelWeChat   ChannelID = "wechat"
	ChannelAgent    ChannelID = "agent"
	ChannelSlack    ChannelID = "slack"
	ChannelMatrix   ChannelID = "matrix"
	ChannelEmail    ChannelID = "email"
)

// Capabilities declares what a channel supports.
type Capabilities struct {
	Threads     bool `json:"threads"`
	Reactions   bool `json:"reactions"`
	EditMessage bool `json:"edit_message"`
	Media       bool `json:"media"`
	Markdown    bool `json:"markdown"`
	MaxTextLen  int  `json:"max_text_len"` // 0 = unlimited
}

// AccountConfig holds per-account credentials and settings for a channel.
type AccountConfig struct {
	AccountID string            `json:"id"`
	Enabled   bool              `json:"enabled"`
	Config    map[string]string `json:"config"`
}

// InboundMessage is what channel adapters push into the hub when a message
// arrives from the external platform.
type InboundMessage struct {
	ChannelID  ChannelID
	AccountID  string
	SenderID   string // platform-native user ID
	SenderName string
	Content    MessageContent
	PlatformID string // platform-native message ID
	ChatID     string // platform-native chat/group ID (used for routing)
	ThreadID   string
	Timestamp  time.Time
}

// OutboundTarget identifies where to deliver on the platform side.
type OutboundTarget struct {
	ChatID    string
	ThreadID  string
	AccountID string
}

// OutboundMessage is what the hub sends to channel adapters for delivery.
type OutboundMessage struct {
	Content   MessageContent
	ReplyToID string // platform-native message ID to reply to
}

// MessageContent is a unified representation of message content across platforms.
type MessageContent struct {
	Text        string       `json:"text,omitempty"`
	HTML        string       `json:"html,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment represents a file or media item in a message.
type Attachment struct {
	Type     string `json:"type"` // "image", "file", "audio", "video"
	URL      string `json:"url"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Channel is the core adapter interface. Each messaging platform implements this.
type Channel interface {
	// ID returns the channel's platform identifier.
	ID() ChannelID

	// Capabilities returns what the channel supports.
	Capabilities() Capabilities

	// Start launches the channel with the given accounts. Inbound messages
	// from the platform should be pushed onto the inbound channel.
	// Start must be non-blocking (launch goroutines internally).
	Start(ctx context.Context, accounts []AccountConfig, inbound chan<- InboundMessage) error

	// Stop gracefully shuts down the channel.
	Stop(ctx context.Context) error

	// Healthy returns true if the channel is operational.
	Healthy() bool

	// Send delivers a message to the external platform.
	Send(ctx context.Context, target OutboundTarget, msg OutboundMessage) error
}
