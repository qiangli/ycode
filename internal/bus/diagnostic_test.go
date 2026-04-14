package bus

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDiagnosticEmitter_Emit(t *testing.T) {
	mb := NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(EventDiagModelUsage)
	defer unsub()

	de := NewDiagnosticEmitter(mb)
	de.EmitModelUsage("sess-1", "claude-sonnet", 100, 50, 10, 5, 0.005, 1234)

	select {
	case ev := <-ch:
		if ev.Type != EventDiagModelUsage {
			t.Fatalf("expected %s, got %s", EventDiagModelUsage, ev.Type)
		}
		if ev.SessionID != "sess-1" {
			t.Fatalf("expected session sess-1, got %s", ev.SessionID)
		}
		var payload DiagnosticEvent
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Attrs["model"] != "claude-sonnet" {
			t.Errorf("expected model claude-sonnet, got %v", payload.Attrs["model"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestDiagnosticEmitter_SessionState(t *testing.T) {
	mb := NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(EventDiagSessionState)
	defer unsub()

	de := NewDiagnosticEmitter(mb)
	de.EmitSessionState("sess-2", "idle", "processing", "turn started")

	select {
	case ev := <-ch:
		var payload DiagnosticEvent
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Attrs["from"] != "idle" || payload.Attrs["to"] != "processing" {
			t.Errorf("unexpected state transition: %v", payload.Attrs)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestDiagnosticEmitter_ToolLoop(t *testing.T) {
	mb := NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(EventDiagToolLoop)
	defer unsub()

	de := NewDiagnosticEmitter(mb)
	de.EmitToolLoop("sess-3", "generic_repeat", 3, "warning")

	select {
	case ev := <-ch:
		var payload DiagnosticEvent
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Attrs["detector_type"] != "generic_repeat" {
			t.Errorf("expected generic_repeat, got %v", payload.Attrs["detector_type"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestDiagnosticEmitter_Heartbeat(t *testing.T) {
	mb := NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(EventDiagHeartbeat)
	defer unsub()

	de := NewDiagnosticEmitter(mb)
	de.EmitHeartbeat(3, map[string]any{"uptime_seconds": 120})

	select {
	case ev := <-ch:
		var payload DiagnosticEvent
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// JSON unmarshals numbers as float64.
		if payload.Attrs["active_sessions"] != float64(3) {
			t.Errorf("expected 3 active sessions, got %v", payload.Attrs["active_sessions"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}
