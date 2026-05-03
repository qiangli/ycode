package conversation

// StreamEventType identifies the kind of streaming event emitted during a conversation turn.
type StreamEventType string

const (
	// EventModelStart is emitted when the LLM begins generating a response.
	EventModelStart StreamEventType = "model.start"
	// EventTextDelta is emitted for each text chunk from the LLM.
	EventTextDelta StreamEventType = "text.delta"
	// EventThinkingDelta is emitted for each thinking/reasoning chunk.
	EventThinkingDelta StreamEventType = "thinking.delta"
	// EventToolStart is emitted when a tool call begins execution.
	EventToolStart StreamEventType = "tool.start"
	// EventToolDelta is emitted for partial output from a streaming tool.
	EventToolDelta StreamEventType = "tool.delta"
	// EventToolEnd is emitted when a tool call completes (success or error).
	EventToolEnd StreamEventType = "tool.end"
	// EventToolError is emitted when a tool call fails.
	EventToolError StreamEventType = "tool.error"
	// EventTurnEnd is emitted when the entire turn (model + tools) completes.
	EventTurnEnd StreamEventType = "turn.end"
	// EventStateUpdate is emitted when conversation state changes (compaction, pruning).
	EventStateUpdate StreamEventType = "state.update"
	// EventCheckpoint is emitted when a checkpoint is persisted.
	EventCheckpoint StreamEventType = "checkpoint"
)

// StreamMode controls which event types a subscriber receives.
type StreamMode int

const (
	// StreamAll delivers all events (default).
	StreamAll StreamMode = 0
	// StreamValues delivers only final state values (text content, tool results).
	StreamValues StreamMode = 1 << iota
	// StreamUpdates delivers state change events (tool start/end, state updates).
	StreamUpdates
	// StreamTools delivers only tool-related events (start, delta, end, error).
	StreamTools
	// StreamDebug delivers all events including internal diagnostics.
	StreamDebug
)

// StreamEvent is a typed event emitted during conversation turns.
type StreamEvent struct {
	Type StreamEventType `json:"type"`
	Data map[string]any  `json:"data,omitempty"`
}

// StreamSubscriber receives filtered stream events.
type StreamSubscriber struct {
	Mode StreamMode
	Ch   chan<- StreamEvent
}

// Accepts returns true if the subscriber's mode includes the given event type.
func (s *StreamSubscriber) Accepts(eventType StreamEventType) bool {
	if s.Mode == StreamAll || s.Mode&StreamDebug != 0 {
		return true
	}
	switch eventType {
	case EventTextDelta, EventThinkingDelta, EventTurnEnd:
		return s.Mode&StreamValues != 0
	case EventToolStart, EventToolDelta, EventToolEnd, EventToolError:
		return s.Mode&StreamTools != 0 || s.Mode&StreamUpdates != 0
	case EventStateUpdate, EventCheckpoint, EventModelStart:
		return s.Mode&StreamUpdates != 0
	default:
		return false
	}
}
