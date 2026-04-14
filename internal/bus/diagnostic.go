package bus

import (
	"encoding/json"
	"time"
)

// Diagnostic event types — structured observability events.
const (
	EventDiagModelUsage   EventType = "diagnostic.model.usage"
	EventDiagSessionState EventType = "diagnostic.session.state"
	EventDiagToolLoop     EventType = "diagnostic.tool.loop"
	EventDiagQueueLane    EventType = "diagnostic.queue.lane"
	EventDiagHeartbeat    EventType = "diagnostic.heartbeat"
	EventDiagSessionStuck EventType = "diagnostic.session.stuck"
)

// DiagnosticEvent is a structured diagnostic payload.
type DiagnosticEvent struct {
	Category  string         `json:"category"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// DiagnosticEmitter publishes structured diagnostic events to the bus.
type DiagnosticEmitter struct {
	bus Bus
}

// NewDiagnosticEmitter creates an emitter that publishes to the given bus.
func NewDiagnosticEmitter(b Bus) *DiagnosticEmitter {
	return &DiagnosticEmitter{bus: b}
}

// Emit publishes a diagnostic event to the bus.
func (de *DiagnosticEmitter) Emit(eventType EventType, sessionID string, attrs map[string]any) {
	payload := DiagnosticEvent{
		Category:  string(eventType),
		SessionID: sessionID,
		Timestamp: time.Now(),
		Attrs:     attrs,
	}
	data, _ := json.Marshal(payload)

	de.bus.Publish(Event{
		ID:        NextEventID(),
		Type:      eventType,
		SessionID: sessionID,
		Timestamp: payload.Timestamp,
		Data:      data,
	})
}

// EmitModelUsage publishes a model usage diagnostic event.
func (de *DiagnosticEmitter) EmitModelUsage(sessionID string, model string, inputTokens, outputTokens int, cacheRead, cacheWrite int, costUSD float64, durationMs int64) {
	de.Emit(EventDiagModelUsage, sessionID, map[string]any{
		"model":         model,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"cache_read":    cacheRead,
		"cache_write":   cacheWrite,
		"cost_usd":      costUSD,
		"duration_ms":   durationMs,
	})
}

// EmitSessionState publishes a session state transition event.
func (de *DiagnosticEmitter) EmitSessionState(sessionID string, from, to, reason string) {
	de.Emit(EventDiagSessionState, sessionID, map[string]any{
		"from":   from,
		"to":     to,
		"reason": reason,
	})
}

// EmitToolLoop publishes a tool loop detection event.
func (de *DiagnosticEmitter) EmitToolLoop(sessionID string, detectorType string, consecutiveCount int, status string) {
	de.Emit(EventDiagToolLoop, sessionID, map[string]any{
		"detector_type":     detectorType,
		"consecutive_count": consecutiveCount,
		"status":            status,
	})
}

// EmitSessionStuck publishes a stuck session detection event.
func (de *DiagnosticEmitter) EmitSessionStuck(sessionID string, age time.Duration, state string) {
	de.Emit(EventDiagSessionStuck, sessionID, map[string]any{
		"age_seconds": age.Seconds(),
		"state":       state,
	})
}

// EmitHeartbeat publishes a periodic health snapshot.
func (de *DiagnosticEmitter) EmitHeartbeat(activeSessions int, attrs map[string]any) {
	merged := map[string]any{
		"active_sessions": activeSessions,
	}
	for k, v := range attrs {
		merged[k] = v
	}
	de.Emit(EventDiagHeartbeat, "", merged)
}
