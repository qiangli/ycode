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

	// Command streaming events (server → client for slash command execution).
	EventCommandProgress EventType = "command.progress" // progress line
	EventCommandDelta    EventType = "command.delta"    // streaming text delta
	EventCommandComplete EventType = "command.complete" // command finished
	EventCommandError    EventType = "command.error"    // command failed

	// Mesh agent events.
	EventDiagReport    EventType = "diagnostic.report"
	EventFixStart      EventType = "fix.start"
	EventFixComplete   EventType = "fix.complete"
	EventFixFailed     EventType = "fix.failed"
	EventLearnComplete EventType = "learn.complete"
	EventResearchDone  EventType = "research.done"
	EventTrainStart    EventType = "train.start"
	EventTrainComplete EventType = "train.complete"

	// Observability alert events. Fired when an alert rule trips. ycode
	// components subscribe for self-healing; Alertmanager continues to
	// route the same alert for human notifications in parallel.
	EventAlertFired EventType = "alert.fired"

	// Canvas / generative-UI events.
	//
	// EventStateUpdate (server → client) carries a generative-UI payload
	// for the /canvas/ route (and any foreign tool subscribing via MCP).
	// The payload is format-discriminated:
	//
	//	{ "format": "a2ui",   "surface": "...", "op": { ... } }
	//	{ "format": "iframe", "widget_id": "...", "html_chunk": "..." }
	//
	// A2UI ops follow the v0.9 spec (createSurface / updateComponents /
	// updateDataModel — see internal/runtime/a2ui). Iframe payloads stream
	// raw HTML fragments into a sandboxed iframe via postMessage.
	//
	// EventStateMutate (client → server) carries the inverse — a user
	// gesture on a rendered surface that the agent should observe:
	//
	//	{ "format": "a2ui",   "surface": "...", "patch": { ... } }
	//	{ "format": "iframe", "widget_id": "...", "event": { ... } }
	//
	// Both directions flow through the same bus + session WebSocket so
	// the canvas is a normal bus participant, not a parallel transport.
	EventStateUpdate EventType = "state.update"
	EventStateMutate EventType = "state.mutate"
)

// Event is a single message flowing through the bus.
type Event struct {
	ID        uint64          `json:"id"`
	Type      EventType       `json:"type"`
	SessionID string          `json:"session_id"`
	GroupID   string          `json:"group_id,omitempty"`
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
