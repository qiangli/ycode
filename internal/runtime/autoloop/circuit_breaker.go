package autoloop

import (
	"sync"
	"time"
)

// CircuitState represents the three states of the circuit breaker.
// Inspired by ralph-claude-code's three-state pattern (Nygard's "Release It!").
type CircuitState int

const (
	// CircuitClosed means the loop is running normally.
	CircuitClosed CircuitState = iota
	// CircuitOpen means the loop is halted due to repeated failures.
	CircuitOpen
	// CircuitHalfOpen means the loop is probing with one iteration to see if recovery occurred.
	CircuitHalfOpen
)

// String returns a human-readable circuit state label.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures the thresholds for opening the circuit.
type CircuitBreakerConfig struct {
	// MaxNoProgress opens the circuit after N consecutive iterations with no progress.
	MaxNoProgress int
	// MaxSameError opens the circuit after N consecutive iterations with the same error.
	MaxSameError int
	// CooldownDuration is how long to wait in Open state before transitioning to HalfOpen.
	CooldownDuration time.Duration
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxNoProgress:    3,
		MaxSameError:     5,
		CooldownDuration: 30 * time.Second,
	}
}

// CircuitBreaker monitors autonomous loop health and halts execution
// when repeated failures are detected. After a cooldown period, it
// transitions to HalfOpen to probe for recovery.
type CircuitBreaker struct {
	mu     sync.Mutex
	config CircuitBreakerConfig
	state  CircuitState

	consecutiveNoProgress int
	consecutiveSameError  int
	lastError             string
	openedAt              time.Time
}

// NewCircuitBreaker creates a circuit breaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: cfg,
		state:  CircuitClosed,
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// AllowIteration checks if the loop should proceed. Returns true if the
// circuit is Closed or HalfOpen. If Open, checks cooldown expiry and
// transitions to HalfOpen if enough time has passed.
func (cb *CircuitBreaker) AllowIteration() bool {
	return cb.allowIterationAt(time.Now())
}

func (cb *CircuitBreaker) allowIterationAt(now time.Time) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed, CircuitHalfOpen:
		return true
	case CircuitOpen:
		if now.Sub(cb.openedAt) >= cb.config.CooldownDuration {
			cb.state = CircuitHalfOpen
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful iteration with progress.
// Resets all failure counters and closes the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveNoProgress = 0
	cb.consecutiveSameError = 0
	cb.lastError = ""
	cb.state = CircuitClosed
}

// RecordNoProgress records an iteration that completed but made no progress.
// Opens the circuit if the threshold is exceeded.
func (cb *CircuitBreaker) RecordNoProgress() {
	cb.recordNoProgressAt(time.Now())
}

func (cb *CircuitBreaker) recordNoProgressAt(now time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveNoProgress++
	if cb.config.MaxNoProgress > 0 && cb.consecutiveNoProgress >= cb.config.MaxNoProgress {
		cb.state = CircuitOpen
		cb.openedAt = now
	}
}

// RecordError records an iteration that failed with an error.
// Consecutive identical errors open the circuit at the threshold.
func (cb *CircuitBreaker) RecordError(errMsg string) {
	cb.recordErrorAt(errMsg, time.Now())
}

func (cb *CircuitBreaker) recordErrorAt(errMsg string, now time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if errMsg == cb.lastError {
		cb.consecutiveSameError++
	} else {
		cb.consecutiveSameError = 1
		cb.lastError = errMsg
	}

	if cb.config.MaxSameError > 0 && cb.consecutiveSameError >= cb.config.MaxSameError {
		cb.state = CircuitOpen
		cb.openedAt = now
	}
}

// Reset forces the circuit back to Closed and clears all counters.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.consecutiveNoProgress = 0
	cb.consecutiveSameError = 0
	cb.lastError = ""
}
