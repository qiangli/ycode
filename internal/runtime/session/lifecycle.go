package session

import (
	"sync"
	"time"
)

// SessionState represents the lifecycle state of a session.
type SessionState int

const (
	StateIdle       SessionState = iota // No active turn
	StateProcessing                     // LLM is generating a response
	StateWaiting                        // Waiting for user input (e.g., permission prompt)
	StateError                          // Turn ended with an error
)

// String returns the state name.
func (s SessionState) String() string {
	switch s {
	case StateProcessing:
		return "processing"
	case StateWaiting:
		return "waiting"
	case StateError:
		return "error"
	default:
		return "idle"
	}
}

// StateTransition records a state change.
type StateTransition struct {
	From      SessionState
	To        SessionState
	Timestamp time.Time
	SessionID string
	Reason    string // optional human-readable reason
}

// LifecycleTracker tracks the lifecycle state of a session.
type LifecycleTracker struct {
	mu        sync.RWMutex
	sessionID string
	state     SessionState
	since     time.Time             // when the current state was entered
	onChange  func(StateTransition) // optional callback
}

// NewLifecycleTracker creates a tracker starting in Idle state.
func NewLifecycleTracker(sessionID string) *LifecycleTracker {
	return &LifecycleTracker{
		sessionID: sessionID,
		state:     StateIdle,
		since:     time.Now(),
	}
}

// SetOnChange sets a callback invoked on every state transition.
// The callback must not block.
func (lt *LifecycleTracker) SetOnChange(fn func(StateTransition)) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.onChange = fn
}

// State returns the current state.
func (lt *LifecycleTracker) State() SessionState {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return lt.state
}

// Since returns when the current state was entered.
func (lt *LifecycleTracker) Since() time.Time {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return lt.since
}

// Transition moves to a new state. If the state hasn't changed, it's a no-op.
func (lt *LifecycleTracker) Transition(to SessionState, reason string) {
	lt.mu.Lock()
	from := lt.state
	if from == to {
		lt.mu.Unlock()
		return
	}
	now := time.Now()
	lt.state = to
	lt.since = now
	onChange := lt.onChange
	lt.mu.Unlock()

	if onChange != nil {
		onChange(StateTransition{
			From:      from,
			To:        to,
			Timestamp: now,
			SessionID: lt.sessionID,
			Reason:    reason,
		})
	}
}

// Duration returns how long the session has been in the current state.
func (lt *LifecycleTracker) Duration() time.Duration {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return time.Since(lt.since)
}
