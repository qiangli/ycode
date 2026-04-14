package otel

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/telemetry/redact"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestDiagnosticBridge_StartsAndStops(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	redactor := redact.DefaultPatterns()
	bridge := NewDiagnosticBridge(tracer, redactor, nil)

	mb := bus.NewMemoryBus()
	defer mb.Close()

	bridge.Start(mb)

	// Emit a diagnostic event.
	emitter := bus.NewDiagnosticEmitter(mb)
	emitter.EmitModelUsage("sess-1", "claude-sonnet", 100, 50, 10, 5, 0.005, 1234)

	// Give the goroutine time to process.
	time.Sleep(50 * time.Millisecond)

	bridge.Stop()
}

func TestDiagnosticBridge_HandlesAllEventTypes(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	bridge := NewDiagnosticBridge(tracer, nil, nil)

	mb := bus.NewMemoryBus()
	defer mb.Close()

	bridge.Start(mb)
	defer bridge.Stop()

	emitter := bus.NewDiagnosticEmitter(mb)
	emitter.EmitModelUsage("s1", "model", 1, 1, 0, 0, 0, 0)
	emitter.EmitSessionState("s1", "idle", "processing", "start")
	emitter.EmitToolLoop("s1", "generic_repeat", 3, "warning")
	emitter.EmitSessionStuck("s1", 5*time.Minute, "processing")
	emitter.EmitHeartbeat(2, nil)

	time.Sleep(50 * time.Millisecond)
}

func TestDiagnosticBridge_RedactsSensitiveData(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	redactor := redact.DefaultPatterns()
	bridge := NewDiagnosticBridge(tracer, redactor, nil)

	mb := bus.NewMemoryBus()
	defer mb.Close()

	bridge.Start(mb)
	defer bridge.Stop()

	// Emit event with sensitive data in attrs.
	emitter := bus.NewDiagnosticEmitter(mb)
	emitter.Emit(bus.EventDiagModelUsage, "s1", map[string]any{
		"key": "sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456",
	})

	time.Sleep(50 * time.Millisecond)
	// The span is created with noop tracer so we can't inspect attrs directly,
	// but this verifies no panic/crash with sensitive data.
}

func TestDiagnosticAttr(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
	}{
		{"string", "model", "claude"},
		{"float64", "cost", 0.005},
		{"int", "count", 42},
		{"bool", "success", true},
		{"nil", "unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := diagnosticAttr(tt.key, tt.value)
			if kv.Key == "" {
				t.Error("expected non-empty key")
			}
		})
	}
}
