package session

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestStuckDetector_DetectsStuckSession(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventDiagSessionStuck)
	defer unsub()

	emitter := bus.NewDiagnosticEmitter(mb)
	sd := NewStuckDetector(StuckDetectorConfig{
		CheckInterval:  10 * time.Millisecond,
		StuckThreshold: 20 * time.Millisecond,
		Emitter:        emitter,
	})

	// Create a tracker in Processing state.
	lt := NewLifecycleTracker("stuck-session")
	lt.Transition(StateProcessing, "test")

	sd.Register("stuck-session", lt)
	sd.Start()
	defer sd.Stop()

	// Wait for the threshold + check interval.
	time.Sleep(50 * time.Millisecond)

	select {
	case ev := <-ch:
		if ev.Type != bus.EventDiagSessionStuck {
			t.Errorf("expected session.stuck event, got %s", ev.Type)
		}
		if ev.SessionID != "stuck-session" {
			t.Errorf("expected stuck-session, got %s", ev.SessionID)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for stuck event")
	}
}

func TestStuckDetector_IgnoresIdleSessions(t *testing.T) {
	sd := NewStuckDetector(StuckDetectorConfig{
		StuckThreshold: 1 * time.Millisecond,
	})

	lt := NewLifecycleTracker("idle-session")
	// State is Idle by default.

	sd.Register("idle-session", lt)

	// Manual check.
	sd.check()

	stuck := sd.StuckSessions()
	if len(stuck) != 0 {
		t.Errorf("expected no stuck sessions for idle state, got %d", len(stuck))
	}
}

func TestStuckDetector_StuckSessions(t *testing.T) {
	sd := NewStuckDetector(StuckDetectorConfig{
		StuckThreshold: 1 * time.Millisecond,
	})

	lt := NewLifecycleTracker("test-session")
	lt.Transition(StateProcessing, "test")

	sd.Register("test-session", lt)

	// Wait past threshold.
	time.Sleep(5 * time.Millisecond)

	stuck := sd.StuckSessions()
	if len(stuck) != 1 {
		t.Fatalf("expected 1 stuck session, got %d", len(stuck))
	}
	if stuck[0].SessionID != "test-session" {
		t.Errorf("expected test-session, got %s", stuck[0].SessionID)
	}
	if stuck[0].State != StateProcessing {
		t.Errorf("expected Processing, got %s", stuck[0].State)
	}
}

func TestStuckDetector_UnregisterRemoves(t *testing.T) {
	sd := NewStuckDetector(StuckDetectorConfig{
		StuckThreshold: 1 * time.Millisecond,
	})

	lt := NewLifecycleTracker("unreg-session")
	lt.Transition(StateProcessing, "test")

	sd.Register("unreg-session", lt)
	sd.Unregister("unreg-session")

	time.Sleep(5 * time.Millisecond)

	stuck := sd.StuckSessions()
	if len(stuck) != 0 {
		t.Errorf("expected 0 stuck after unregister, got %d", len(stuck))
	}
}
