package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/telemetry"
)

// OTELSink implements telemetry.Sink by forwarding events as OTEL spans and metrics.
// It bridges existing SessionTracer/AnalyticsCollector output to the OTEL pipeline.
type OTELSink struct {
	tracer trace.Tracer
	inst   *Instruments
}

// NewOTELSink creates a sink that translates telemetry events to OTEL signals.
func NewOTELSink(p *Provider) *OTELSink {
	return &OTELSink{
		tracer: p.Tracer("ycode.bridge"),
		inst:   p.Instruments,
	}
}

// Emit translates a telemetry event into OTEL spans/metrics.
func (s *OTELSink) Emit(event *telemetry.Event) error {
	if event == nil {
		return nil
	}

	switch event.Type {
	case "trace_complete":
		s.emitTraceComplete(event)
	case "prompt":
		s.emitPrompt(event)
	case "tool_use":
		s.emitToolUse(event)
	case "prompt_cache":
		s.emitCacheEvent(event)
	}

	return nil
}

// Close is a no-op; shutdown is handled by the Provider.
func (s *OTELSink) Close() error { return nil }

func (s *OTELSink) emitTraceComplete(event *telemetry.Event) {
	record, ok := event.Data.(telemetry.SessionTraceRecord)
	if !ok {
		return
	}
	_, span := s.tracer.Start(context.Background(), "ycode.session."+record.Type, //nolint:staticcheck
		trace.WithTimestamp(record.StartedAt),
		trace.WithAttributes(
			attribute.String("trace.id", record.ID),
			attribute.String("trace.name", record.Name),
		),
	)
	if record.Error != "" {
		span.SetStatus(codes.Error, record.Error)
	}
	span.End(trace.WithTimestamp(record.CompletedAt))
}

func (s *OTELSink) emitPrompt(event *telemetry.Event) {
	ae, ok := event.Data.(*telemetry.AnalyticsEvent)
	if !ok {
		return
	}
	_, span := s.tracer.Start(context.Background(), "ycode.api.call", //nolint:staticcheck
		trace.WithTimestamp(event.Timestamp),
		trace.WithAttributes(
			AttrLLMModel.String(ae.Model),
			AttrLLMTokensInput.Int(ae.TokensIn),
			AttrLLMTokensOutput.Int(ae.TokensOut),
			AttrLLMSuccess.Bool(ae.Success),
		),
	)
	if ae.Error != "" {
		span.SetStatus(codes.Error, ae.Error)
	}
	span.End()
}

func (s *OTELSink) emitToolUse(event *telemetry.Event) {
	ae, ok := event.Data.(*telemetry.AnalyticsEvent)
	if !ok {
		return
	}
	_, span := s.tracer.Start(context.Background(), "ycode.tool.call", //nolint:staticcheck
		trace.WithTimestamp(event.Timestamp),
		trace.WithAttributes(
			AttrToolName.String(ae.ToolName),
			AttrToolSuccess.Bool(ae.Success),
		),
	)
	if ae.Error != "" {
		span.SetStatus(codes.Error, ae.Error)
	}
	span.End()
}

func (s *OTELSink) emitCacheEvent(event *telemetry.Event) {
	ce, ok := event.Data.(*telemetry.PromptCacheEvent)
	if !ok {
		return
	}
	_, span := s.tracer.Start(context.Background(), "ycode.prompt_cache", //nolint:staticcheck
		trace.WithTimestamp(event.Timestamp),
		trace.WithAttributes(
			attribute.Bool("cache.hit", ce.CacheHit),
			attribute.Bool("cache.write", ce.CacheWrite),
			attribute.Bool("cache.miss", ce.CacheMiss),
			attribute.Int("cache.tokens_saved", ce.TokensSaved),
		),
	)
	span.End()
}
