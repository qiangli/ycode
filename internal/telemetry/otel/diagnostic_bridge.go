package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/telemetry/redact"
)

// DiagnosticBridge subscribes to bus diagnostic events and maps them to OTEL spans.
type DiagnosticBridge struct {
	tracer   trace.Tracer
	redactor *redact.Redactor
	unsub    func()
	logger   *slog.Logger
}

// NewDiagnosticBridge creates a bridge that maps diagnostic events to OTEL spans.
// Call Start() to begin consuming events and Stop() to shut down.
func NewDiagnosticBridge(tracer trace.Tracer, redactor *redact.Redactor, logger *slog.Logger) *DiagnosticBridge {
	if logger == nil {
		logger = slog.Default()
	}
	if redactor == nil {
		redactor = redact.DefaultPatterns()
	}
	return &DiagnosticBridge{
		tracer:   tracer,
		redactor: redactor,
		logger:   logger,
	}
}

// Start subscribes to all diagnostic event types and begins mapping them to spans.
func (db *DiagnosticBridge) Start(b bus.Bus) {
	ch, unsub := b.Subscribe(
		bus.EventDiagModelUsage,
		bus.EventDiagSessionState,
		bus.EventDiagToolLoop,
		bus.EventDiagQueueLane,
		bus.EventDiagHeartbeat,
		bus.EventDiagSessionStuck,
	)
	db.unsub = unsub

	go func() {
		for ev := range ch {
			db.handleEvent(ev)
		}
	}()
}

// Stop unsubscribes from the bus.
func (db *DiagnosticBridge) Stop() {
	if db.unsub != nil {
		db.unsub()
	}
}

func (db *DiagnosticBridge) handleEvent(ev bus.Event) {
	var diag bus.DiagnosticEvent
	if err := json.Unmarshal(ev.Data, &diag); err != nil {
		db.logger.Warn("failed to unmarshal diagnostic event", "error", err)
		return
	}

	spanName := fmt.Sprintf("diagnostic.%s", ev.Type)

	// Create a short-lived span representing the diagnostic event.
	ctx := context.Background()
	_, span := db.tracer.Start(ctx, spanName,
		trace.WithTimestamp(diag.Timestamp),
	)

	// Add session ID attribute.
	if diag.SessionID != "" {
		span.SetAttributes(attribute.String("session.id", diag.SessionID))
	}

	// Add event category.
	span.SetAttributes(attribute.String("diagnostic.category", diag.Category))

	// Map diagnostic attrs to span attributes, with redaction.
	redacted := db.redactor.RedactMap(diag.Attrs)
	for k, v := range redacted {
		span.SetAttributes(diagnosticAttr(k, v))
	}

	span.End(trace.WithTimestamp(diag.Timestamp))
}

// diagnosticAttr converts a key-value pair to an OTEL attribute.
func diagnosticAttr(key string, value any) attribute.KeyValue {
	attrKey := attribute.Key("diagnostic." + key)
	switch v := value.(type) {
	case string:
		return attrKey.String(v)
	case float64:
		return attrKey.Float64(v)
	case int:
		return attrKey.Int(v)
	case int64:
		return attrKey.Int64(v)
	case bool:
		return attrKey.Bool(v)
	default:
		return attrKey.String(fmt.Sprintf("%v", v))
	}
}
