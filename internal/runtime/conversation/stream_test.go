package conversation

import "testing"

func TestStreamSubscriberAccepts(t *testing.T) {
	tests := []struct {
		name      string
		mode      StreamMode
		eventType StreamEventType
		want      bool
	}{
		{"all accepts text delta", StreamAll, EventTextDelta, true},
		{"all accepts tool start", StreamAll, EventToolStart, true},
		{"all accepts checkpoint", StreamAll, EventCheckpoint, true},
		{"values accepts text delta", StreamValues, EventTextDelta, true},
		{"values accepts turn end", StreamValues, EventTurnEnd, true},
		{"values rejects tool start", StreamValues, EventToolStart, false},
		{"values rejects checkpoint", StreamValues, EventCheckpoint, false},
		{"tools accepts tool start", StreamTools, EventToolStart, true},
		{"tools accepts tool end", StreamTools, EventToolEnd, true},
		{"tools accepts tool delta", StreamTools, EventToolDelta, true},
		{"tools accepts tool error", StreamTools, EventToolError, true},
		{"tools rejects text delta", StreamTools, EventTextDelta, false},
		{"updates accepts tool start", StreamUpdates, EventToolStart, true},
		{"updates accepts state update", StreamUpdates, EventStateUpdate, true},
		{"updates accepts checkpoint", StreamUpdates, EventCheckpoint, true},
		{"updates accepts model start", StreamUpdates, EventModelStart, true},
		{"updates rejects text delta", StreamUpdates, EventTextDelta, false},
		{"debug accepts everything", StreamDebug, EventTextDelta, true},
		{"debug accepts tool start", StreamDebug, EventToolStart, true},
		{"debug accepts checkpoint", StreamDebug, EventCheckpoint, true},
		{"combined values+tools", StreamValues | StreamTools, EventTextDelta, true},
		{"combined values+tools", StreamValues | StreamTools, EventToolStart, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &StreamSubscriber{Mode: tt.mode}
			if got := sub.Accepts(tt.eventType); got != tt.want {
				t.Errorf("Accepts(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestStreamEventTypes(t *testing.T) {
	// Verify event type constants are non-empty and unique.
	types := []StreamEventType{
		EventModelStart, EventTextDelta, EventThinkingDelta,
		EventToolStart, EventToolDelta, EventToolEnd, EventToolError,
		EventTurnEnd, EventStateUpdate, EventCheckpoint,
	}
	seen := make(map[StreamEventType]bool)
	for _, et := range types {
		if et == "" {
			t.Error("empty event type found")
		}
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true
	}
}
