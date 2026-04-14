package conversation

import (
	"testing"

	"github.com/qiangli/ycode/internal/bus"
)

func TestEnhancedLoopDetector_RecordResponse(t *testing.T) {
	d := NewEnhancedLoopDetector(EnhancedLoopDetectorConfig{})

	// First few responses should be fine.
	for i := range 2 {
		if s := d.RecordResponse("same response"); s != LoopNone {
			t.Errorf("iteration %d: expected None, got %s", i, s)
		}
	}

	// Third similar response triggers warning.
	if s := d.RecordResponse("same response"); s != LoopWarning {
		t.Errorf("expected Warning, got %s", s)
	}
}

func TestEnhancedLoopDetector_CircuitBreaker(t *testing.T) {
	d := NewEnhancedLoopDetector(EnhancedLoopDetectorConfig{
		CircuitBreakerMax: 5,
	})

	for i := range 4 {
		if s := d.RecordToolCall("bash"); s != LoopNone {
			t.Errorf("call %d: expected None, got %s", i, s)
		}
	}

	if s := d.RecordToolCall("bash"); s != LoopBreak {
		t.Errorf("expected Break at max, got %s", s)
	}
}

func TestEnhancedLoopDetector_PingPong(t *testing.T) {
	d := NewEnhancedLoopDetector(EnhancedLoopDetectorConfig{})

	// Build A-B-A-B pattern.
	d.RecordToolCall("read_file")
	d.RecordToolCall("write_file")
	d.RecordToolCall("read_file")
	d.RecordToolCall("write_file")

	// 2 consecutive pairs → warning.
	// A-B-A-B: pairs = 2

	// Third pair triggers break.
	d.RecordToolCall("read_file")
	s := d.RecordToolCall("write_file")
	if s != LoopBreak {
		t.Errorf("expected Break for 3 ping-pong pairs, got %s", s)
	}
}

func TestEnhancedLoopDetector_ResetTurn(t *testing.T) {
	d := NewEnhancedLoopDetector(EnhancedLoopDetectorConfig{
		CircuitBreakerMax: 3,
	})

	d.RecordToolCall("bash")
	d.RecordToolCall("bash")
	d.ResetTurn()

	if d.TotalToolCalls() != 0 {
		t.Errorf("expected 0 after reset, got %d", d.TotalToolCalls())
	}

	// Should be able to make calls again.
	if s := d.RecordToolCall("bash"); s != LoopNone {
		t.Errorf("expected None after reset, got %s", s)
	}
}

func TestEnhancedLoopDetector_WithEmitter(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	ch, unsub := mb.Subscribe(bus.EventDiagToolLoop)
	defer unsub()

	emitter := bus.NewDiagnosticEmitter(mb)
	d := NewEnhancedLoopDetector(EnhancedLoopDetectorConfig{
		SessionID:         "test-sess",
		Emitter:           emitter,
		CircuitBreakerMax: 3,
	})

	// Trigger circuit breaker.
	d.RecordToolCall("a")
	d.RecordToolCall("b")
	d.RecordToolCall("c")

	// Should have emitted an event.
	select {
	case ev := <-ch:
		if ev.Type != bus.EventDiagToolLoop {
			t.Errorf("expected tool.loop event, got %s", ev.Type)
		}
	default:
		t.Error("expected diagnostic event to be emitted")
	}
}
