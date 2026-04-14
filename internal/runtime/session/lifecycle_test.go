package session

import (
	"sync"
	"testing"
	"time"
)

func TestLifecycleTracker_Transition(t *testing.T) {
	lt := NewLifecycleTracker("test-session")

	if lt.State() != StateIdle {
		t.Fatalf("expected Idle, got %s", lt.State())
	}

	var captured []StateTransition
	var mu sync.Mutex
	lt.SetOnChange(func(tr StateTransition) {
		mu.Lock()
		captured = append(captured, tr)
		mu.Unlock()
	})

	lt.Transition(StateProcessing, "turn started")
	if lt.State() != StateProcessing {
		t.Fatalf("expected Processing, got %s", lt.State())
	}

	// No-op transition.
	lt.Transition(StateProcessing, "duplicate")

	lt.Transition(StateWaiting, "permission prompt")
	lt.Transition(StateProcessing, "permission granted")
	lt.Transition(StateIdle, "turn complete")

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 4 {
		t.Fatalf("expected 4 transitions, got %d", len(captured))
	}
	if captured[0].From != StateIdle || captured[0].To != StateProcessing {
		t.Errorf("transition 0: %s→%s", captured[0].From, captured[0].To)
	}
	if captured[3].To != StateIdle {
		t.Errorf("transition 3: expected Idle, got %s", captured[3].To)
	}
}

func TestLifecycleTracker_Duration(t *testing.T) {
	lt := NewLifecycleTracker("test-session")
	time.Sleep(5 * time.Millisecond)
	if lt.Duration() < 5*time.Millisecond {
		t.Error("expected duration >= 5ms")
	}
}

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{StateIdle, "idle"},
		{StateProcessing, "processing"},
		{StateWaiting, "waiting"},
		{StateError, "error"},
		{SessionState(99), "idle"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
