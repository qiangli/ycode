package telemetry

import "time"

// AnalyticsEvent records user interaction analytics.
type AnalyticsEvent struct {
	SessionID   string    `json:"session_id"`
	EventType   string    `json:"event_type"` // prompt, tool_use, command, error, compaction
	Timestamp   time.Time `json:"timestamp"`
	Model       string    `json:"model,omitempty"`
	ToolName    string    `json:"tool_name,omitempty"`
	CommandName string    `json:"command_name,omitempty"`
	Duration    string    `json:"duration,omitempty"`
	TokensIn    int       `json:"tokens_in,omitempty"`
	TokensOut   int       `json:"tokens_out,omitempty"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
}

// PromptCacheEvent records prompt caching behavior.
type PromptCacheEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Model       string    `json:"model"`
	CacheHit    bool      `json:"cache_hit"`
	CacheWrite  bool      `json:"cache_write"`
	CacheMiss   bool      `json:"cache_miss"`
	CacheBreak  bool      `json:"cache_break"`
	TokensSaved int       `json:"tokens_saved,omitempty"`
	TokensDelta int       `json:"tokens_delta,omitempty"`
	FingerPrint string    `json:"fingerprint,omitempty"`
}

// AnalyticsCollector aggregates analytics events.
type AnalyticsCollector struct {
	sink      Sink
	sessionID string
}

// NewAnalyticsCollector creates an analytics collector.
func NewAnalyticsCollector(sink Sink, sessionID string) *AnalyticsCollector {
	return &AnalyticsCollector{
		sink:      sink,
		sessionID: sessionID,
	}
}

// RecordPrompt records a prompt event.
func (ac *AnalyticsCollector) RecordPrompt(model string, tokensIn, tokensOut int, duration string, err error) {
	event := &AnalyticsEvent{
		SessionID: ac.sessionID,
		EventType: "prompt",
		Timestamp: time.Now(),
		Model:     model,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		Duration:  duration,
		Success:   err == nil,
	}
	if err != nil {
		event.Error = err.Error()
	}
	ac.emit(event)
}

// RecordToolUse records a tool invocation event.
func (ac *AnalyticsCollector) RecordToolUse(toolName string, duration string, success bool, errMsg string) {
	event := &AnalyticsEvent{
		SessionID: ac.sessionID,
		EventType: "tool_use",
		Timestamp: time.Now(),
		ToolName:  toolName,
		Duration:  duration,
		Success:   success,
		Error:     errMsg,
	}
	ac.emit(event)
}

// RecordCommand records a slash command event.
func (ac *AnalyticsCollector) RecordCommand(commandName string, success bool) {
	event := &AnalyticsEvent{
		SessionID:   ac.sessionID,
		EventType:   "command",
		Timestamp:   time.Now(),
		CommandName: commandName,
		Success:     success,
	}
	ac.emit(event)
}

// RecordCacheEvent records a prompt cache event.
func (ac *AnalyticsCollector) RecordCacheEvent(evt *PromptCacheEvent) {
	if ac.sink != nil {
		_ = ac.sink.Emit(&Event{
			Type:      "prompt_cache",
			Timestamp: evt.Timestamp,
			Data:      evt,
		})
	}
}

func (ac *AnalyticsCollector) emit(analyticsEvent *AnalyticsEvent) {
	if ac.sink != nil {
		_ = ac.sink.Emit(&Event{
			Type:      analyticsEvent.EventType,
			Timestamp: analyticsEvent.Timestamp,
			Data:      analyticsEvent,
		})
	}
}
