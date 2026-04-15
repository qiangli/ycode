package adapters

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// MockChannel is a test double that implements Channel. It records
// outbound messages and can inject inbound messages for testing.
type MockChannel struct {
	id      channel.ChannelID
	healthy atomic.Bool
	inbound chan<- channel.InboundMessage
	mu      sync.Mutex
	Sent    []MockSent // recorded outbound messages
}

// MockSent records a single outbound send call.
type MockSent struct {
	Target channel.OutboundTarget
	Msg    channel.OutboundMessage
}

// NewMockChannel creates a mock channel with the given ID.
func NewMockChannel(id channel.ChannelID) *MockChannel {
	return &MockChannel{id: id}
}

func (m *MockChannel) ID() channel.ChannelID { return m.id }

func (m *MockChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     true,
		Reactions:   true,
		EditMessage: true,
		Media:       true,
		Markdown:    true,
	}
}

func (m *MockChannel) Start(_ context.Context, _ []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	m.inbound = inbound
	m.healthy.Store(true)
	return nil
}

func (m *MockChannel) Stop(_ context.Context) error {
	m.healthy.Store(false)
	return nil
}

func (m *MockChannel) Healthy() bool { return m.healthy.Load() }

func (m *MockChannel) Send(_ context.Context, target channel.OutboundTarget, msg channel.OutboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, MockSent{Target: target, Msg: msg})
	return nil
}

// InjectInbound simulates an inbound message from this channel.
func (m *MockChannel) InjectInbound(msg channel.InboundMessage) {
	if m.inbound != nil {
		m.inbound <- msg
	}
}

// SentMessages returns a copy of all recorded outbound messages.
func (m *MockChannel) SentMessages() []MockSent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]MockSent, len(m.Sent))
	copy(cp, m.Sent)
	return cp
}
