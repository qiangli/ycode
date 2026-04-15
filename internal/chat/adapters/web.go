package adapters

import (
	"context"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// WebChannel is the built-in web channel. It doesn't connect to any external
// platform — messages come from the web UI via WebSocket/REST and are pushed
// into the hub's inbound pipeline by the Hub's HTTP handler directly.
//
// The Send method is a no-op because outbound messages to web clients are
// handled by the Hub's WebSocket broadcast, not by this adapter.
type WebChannel struct {
	healthy atomic.Bool
}

// NewWebChannel creates a web channel adapter.
func NewWebChannel() *WebChannel {
	return &WebChannel{}
}

func (w *WebChannel) ID() channel.ChannelID { return channel.ChannelWeb }

func (w *WebChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     true,
		Reactions:   true,
		EditMessage: true,
		Media:       true,
		Markdown:    true,
		MaxTextLen:  0, // unlimited
	}
}

func (w *WebChannel) Start(_ context.Context, _ []channel.AccountConfig, _ chan<- channel.InboundMessage) error {
	w.healthy.Store(true)
	return nil
}

func (w *WebChannel) Stop(_ context.Context) error {
	w.healthy.Store(false)
	return nil
}

func (w *WebChannel) Healthy() bool { return w.healthy.Load() }

// Send is a no-op for the web channel. Outbound delivery to web clients
// is handled by the Hub's WebSocket broadcast.
func (w *WebChannel) Send(_ context.Context, _ channel.OutboundTarget, _ channel.OutboundMessage) error {
	return nil
}
