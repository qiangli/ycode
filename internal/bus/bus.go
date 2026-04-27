package bus

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

// EventType identifies the kind of event flowing through the bus.
type EventType string

const (
	EventTurnStart        EventType = "turn.start"
	EventTextDelta        EventType = "text.delta"
	EventThinkingDelta    EventType = "thinking.delta"
	EventToolUseStart     EventType = "tool_use.start"
	EventToolProgress     EventType = "tool.progress"
	EventToolResult       EventType = "tool.result"
	EventTurnComplete     EventType = "turn.complete"
	EventTurnError        EventType = "turn.error"
	EventPermissionReq    EventType = "permission.request"
	EventPermissionRes    EventType = "permission.response"
	EventUsageUpdate      EventType = "usage.update"
	EventSessionUpdate    EventType = "session.update"
	EventTranscriptUpdate EventType = "transcript.update"
	EventMessageSend      EventType = "message.send"
	EventTurnCancel       EventType = "turn.cancel"
	EventAgentStart       EventType = "agent.start"    // subagent spawned
	EventAgentProgress    EventType = "agent.progress" // subagent tool/turn update
	EventAgentComplete    EventType = "agent.complete" // subagent finished
	EventAgentHandoff     EventType = "agent.handoff"  // control transfer between agents
	EventAgentMessage     EventType = "agent.message"  // inter-agent messaging
	EventFlowStep         EventType = "flow.step"      // flow orchestration step

	// Mesh agent events.
	EventDiagReport    EventType = "diagnostic.report"
	EventFixStart      EventType = "fix.start"
	EventFixComplete   EventType = "fix.complete"
	EventFixFailed     EventType = "fix.failed"
	EventLearnComplete EventType = "learn.complete"
	EventResearchDone  EventType = "research.done"
	EventTrainStart    EventType = "train.start"
	EventTrainComplete EventType = "train.complete"
)

// Event is a single message flowing through the bus.
type Event struct {
	ID        uint64          `json:"id"`
	Type      EventType       `json:"type"`
	SessionID string          `json:"session_id"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// Bus is the publish/subscribe interface for streaming events.
// Implementations: MemoryBus (in-process), NATSBus (distributed).
type Bus interface {
	// Publish sends an event to all matching subscribers.
	Publish(event Event)

	// Subscribe returns a channel of events and an unsubscribe function.
	// If filter types are provided, only matching events are delivered.
	// The returned channel is closed when unsubscribe is called.
	Subscribe(filter ...EventType) (ch <-chan Event, unsubscribe func())

	// Close shuts down the bus and releases resources.
	Close() error
}

// MessageInput is the payload for sending a message to a session.
// Defined here (in the bus package) to avoid import cycles between
// cli, service, and client packages.
type MessageInput struct {
	Text  string            `json:"text"`
	Files []string          `json:"files,omitempty"`
	Extra map[string]string `json:"extra,omitempty"`
}

// global monotonic event ID counter shared across all bus instances.
var globalEventID atomic.Uint64

// NextEventID returns the next monotonic event ID.
func NextEventID() uint64 {
	return globalEventID.Add(1)
}
