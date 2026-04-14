package health

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestHeartbeat_EmitsPeriodically(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventDiagHeartbeat)
	defer unsub()

	emitter := bus.NewDiagnosticEmitter(mb)
	hb := NewHeartbeat(HeartbeatConfig{
		Interval: 20 * time.Millisecond,
		Emitter:  emitter,
	}, func() int { return 3 })

	hb.Start()
	defer hb.Stop()

	// Wait for at least one heartbeat.
	select {
	case ev := <-ch:
		if ev.Type != bus.EventDiagHeartbeat {
			t.Errorf("expected heartbeat event, got %s", ev.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for heartbeat")
	}
}

func TestHeartbeat_IncludesRuntimeMetrics(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventDiagHeartbeat)
	defer unsub()

	emitter := bus.NewDiagnosticEmitter(mb)
	hb := NewHeartbeat(HeartbeatConfig{
		Interval: 10 * time.Millisecond,
		Emitter:  emitter,
	}, nil)

	hb.Start()
	defer hb.Stop()

	select {
	case <-ch:
		// Event received — basic validation that it didn't panic.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout")
	}
}

func TestHeartbeat_CustomGauge(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventDiagHeartbeat)
	defer unsub()

	emitter := bus.NewDiagnosticEmitter(mb)
	hb := NewHeartbeat(HeartbeatConfig{
		Interval: 10 * time.Millisecond,
		Emitter:  emitter,
	}, nil)

	hb.RegisterGauge("queue_depth", func() any { return 42 })
	hb.Start()
	defer hb.Stop()

	select {
	case <-ch:
		// Custom gauge was collected without panic.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout")
	}
}

func TestHeartbeat_StopsCleanly(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	emitter := bus.NewDiagnosticEmitter(mb)
	hb := NewHeartbeat(HeartbeatConfig{
		Interval: 10 * time.Millisecond,
		Emitter:  emitter,
	}, nil)

	hb.Start()
	hb.Stop()
	// If Stop() doesn't deadlock, the test passes.
}

func TestHeartbeat_DefaultInterval(t *testing.T) {
	hb := NewHeartbeat(HeartbeatConfig{}, nil)
	if hb.cfg.Interval != 30*time.Second {
		t.Errorf("expected default 30s interval, got %v", hb.cfg.Interval)
	}
}
