package conversation

import (
	"fmt"
	"sync"
	"time"
)

// AgentMessage represents an inter-agent message.
type AgentMessage struct {
	From     string            `json:"from"`
	To       string            `json:"to"`
	Type     AgentMessageType  `json:"type"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	SentAt   time.Time         `json:"sent_at"`
}

// AgentMessageType classifies inter-agent messages.
type AgentMessageType string

const (
	MessageTypeText             AgentMessageType = "text"
	MessageTypeShutdownRequest  AgentMessageType = "shutdown_request"
	MessageTypeShutdownResponse AgentMessageType = "shutdown_response"
	MessageTypePlanApproval     AgentMessageType = "plan_approval"
)

// MessageRouter enables named inter-agent communication with message queueing.
// Agents register by name and can send messages to specific agents or broadcast.
//
// Inspired by Claude Code's SendMessageTool with named routing and broadcast.
type MessageRouter struct {
	mu       sync.RWMutex
	channels map[string]chan AgentMessage
	bufSize  int
}

// NewMessageRouter creates a new message router.
func NewMessageRouter(bufSize int) *MessageRouter {
	if bufSize <= 0 {
		bufSize = 100
	}
	return &MessageRouter{
		channels: make(map[string]chan AgentMessage),
		bufSize:  bufSize,
	}
}

// Register adds an agent to the router. Returns the message channel for receiving.
func (mr *MessageRouter) Register(name string) <-chan AgentMessage {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	ch := make(chan AgentMessage, mr.bufSize)
	mr.channels[name] = ch
	return ch
}

// Unregister removes an agent from the router and closes its channel.
func (mr *MessageRouter) Unregister(name string) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if ch, ok := mr.channels[name]; ok {
		close(ch)
		delete(mr.channels, name)
	}
}

// Send routes a message to a specific named agent.
// Returns an error if the target is not registered.
func (mr *MessageRouter) Send(msg AgentMessage) error {
	mr.mu.RLock()
	ch, ok := mr.channels[msg.To]
	mr.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %q not registered", msg.To)
	}

	msg.SentAt = time.Now()

	select {
	case ch <- msg:
		return nil
	default:
		return fmt.Errorf("message queue full for agent %q", msg.To)
	}
}

// Broadcast sends a message to all registered agents except the sender.
func (mr *MessageRouter) Broadcast(msg AgentMessage) int {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	msg.SentAt = time.Now()
	sent := 0

	for name, ch := range mr.channels {
		if name == msg.From {
			continue
		}
		select {
		case ch <- msg:
			sent++
		default:
			// Skip agents with full queues.
		}
	}
	return sent
}

// RegisteredAgents returns the list of registered agent names.
func (mr *MessageRouter) RegisteredAgents() []string {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	names := make([]string, 0, len(mr.channels))
	for name := range mr.channels {
		names = append(names, name)
	}
	return names
}

// Drain reads all pending messages from an agent's channel without blocking.
func (mr *MessageRouter) Drain(name string) []AgentMessage {
	mr.mu.RLock()
	ch, ok := mr.channels[name]
	mr.mu.RUnlock()

	if !ok {
		return nil
	}

	var messages []AgentMessage
	for {
		select {
		case msg, open := <-ch:
			if !open {
				return messages
			}
			messages = append(messages, msg)
		default:
			return messages
		}
	}
}
