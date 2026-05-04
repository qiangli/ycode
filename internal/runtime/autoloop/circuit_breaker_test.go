package autoloop

import (
	"testing"
	"time"
)

func TestCircuitBreaker_StartsClosedAndAllows(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	if cb.State() != CircuitClosed {
		t.Errorf("initial state = %v, want closed", cb.State())
	}
	if !cb.AllowIteration() {
		t.Error("closed circuit should allow iteration")
	}
}

func TestCircuitBreaker_OpensOnNoProgress(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxNoProgress: 3, CooldownDuration: time.Minute}
	cb := NewCircuitBreaker(cfg)

	cb.RecordNoProgress()
	cb.RecordNoProgress()

	if cb.State() != CircuitClosed {
		t.Errorf("after 2 no-progress: state = %v, want closed", cb.State())
	}

	cb.RecordNoProgress() // 3rd → opens

	if cb.State() != CircuitOpen {
		t.Errorf("after 3 no-progress: state = %v, want open", cb.State())
	}
	if cb.AllowIteration() {
		t.Error("open circuit should deny iteration")
	}
}

func TestCircuitBreaker_OpensOnSameError(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxSameError: 2, CooldownDuration: time.Minute}
	cb := NewCircuitBreaker(cfg)

	cb.RecordError("build failed: missing import")
	if cb.State() != CircuitClosed {
		t.Errorf("after 1 error: state = %v, want closed", cb.State())
	}

	cb.RecordError("build failed: missing import") // same error → opens

	if cb.State() != CircuitOpen {
		t.Errorf("after 2 same errors: state = %v, want open", cb.State())
	}
}

func TestCircuitBreaker_DifferentErrorResets(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxSameError: 3, CooldownDuration: time.Minute}
	cb := NewCircuitBreaker(cfg)

	cb.RecordError("error A")
	cb.RecordError("error A")
	cb.RecordError("error B") // different → resets counter

	if cb.State() != CircuitClosed {
		t.Errorf("different error should reset counter, state = %v, want closed", cb.State())
	}
}

func TestCircuitBreaker_CooldownRecovery(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxNoProgress: 1, CooldownDuration: 10 * time.Second}
	cb := NewCircuitBreaker(cfg)

	now := time.Now()
	cb.recordNoProgressAt(now) // opens

	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}

	// Before cooldown: still denied.
	if cb.allowIterationAt(now.Add(5 * time.Second)) {
		t.Error("should deny before cooldown expires")
	}

	// After cooldown: transitions to HalfOpen.
	if !cb.allowIterationAt(now.Add(15 * time.Second)) {
		t.Error("should allow after cooldown (half-open)")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state = %v, want half_open", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxNoProgress: 1, CooldownDuration: time.Millisecond}
	cb := NewCircuitBreaker(cfg)

	cb.RecordNoProgress() // opens
	time.Sleep(2 * time.Millisecond)
	cb.AllowIteration() // transitions to half-open

	cb.RecordSuccess() // probe succeeded → closes

	if cb.State() != CircuitClosed {
		t.Errorf("state = %v, want closed after half-open success", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxNoProgress: 1, CooldownDuration: time.Millisecond}
	cb := NewCircuitBreaker(cfg)

	cb.RecordNoProgress() // opens
	time.Sleep(2 * time.Millisecond)
	cb.AllowIteration() // half-open

	cb.RecordNoProgress() // probe failed → reopens

	if cb.State() != CircuitOpen {
		t.Errorf("state = %v, want open after half-open failure", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsCounters(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxNoProgress: 3, MaxSameError: 3, CooldownDuration: time.Minute}
	cb := NewCircuitBreaker(cfg)

	cb.RecordNoProgress()
	cb.RecordNoProgress()
	cb.RecordError("some error")
	cb.RecordError("some error")

	cb.RecordSuccess() // resets everything

	// Should need 3 more no-progress to open.
	cb.RecordNoProgress()
	cb.RecordNoProgress()
	if cb.State() != CircuitClosed {
		t.Errorf("state = %v, want closed (counters were reset)", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxNoProgress: 1, CooldownDuration: time.Hour}
	cb := NewCircuitBreaker(cfg)

	cb.RecordNoProgress() // opens
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("after reset: state = %v, want closed", cb.State())
	}
	if !cb.AllowIteration() {
		t.Error("after reset: should allow iteration")
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		s    CircuitState
		want string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half_open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
