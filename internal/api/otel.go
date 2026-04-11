package api

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentedProvider wraps a Provider with OTEL tracing and metrics.
type InstrumentedProvider struct {
	inner  Provider
	tracer trace.Tracer
	inst   *APIInstruments
}

// APIInstruments holds pre-created metric instruments for API calls.
type APIInstruments struct {
	CallDuration metric.Float64Histogram
	CallTotal    metric.Int64Counter
	TokensInput  metric.Int64Counter
	TokensOutput metric.Int64Counter
	CacheRead    metric.Int64Counter
	CacheWrite   metric.Int64Counter
	CostDollars  metric.Float64Counter
	ContextUsed  metric.Int64Gauge
}

// NewAPIInstruments creates metric instruments for API call tracking.
func NewAPIInstruments(m metric.Meter) (*APIInstruments, error) {
	var inst APIInstruments
	var err error

	if inst.CallDuration, err = m.Float64Histogram("ycode.llm.call.duration",
		metric.WithUnit("ms"), metric.WithDescription("LLM API call latency")); err != nil {
		return nil, err
	}
	if inst.CallTotal, err = m.Int64Counter("ycode.llm.call.total",
		metric.WithDescription("Total LLM API calls")); err != nil {
		return nil, err
	}
	if inst.TokensInput, err = m.Int64Counter("ycode.llm.tokens.input",
		metric.WithUnit("tokens")); err != nil {
		return nil, err
	}
	if inst.TokensOutput, err = m.Int64Counter("ycode.llm.tokens.output",
		metric.WithUnit("tokens")); err != nil {
		return nil, err
	}
	if inst.CacheRead, err = m.Int64Counter("ycode.llm.tokens.cache_read",
		metric.WithUnit("tokens")); err != nil {
		return nil, err
	}
	if inst.CacheWrite, err = m.Int64Counter("ycode.llm.tokens.cache_write",
		metric.WithUnit("tokens")); err != nil {
		return nil, err
	}
	if inst.CostDollars, err = m.Float64Counter("ycode.llm.cost.dollars",
		metric.WithUnit("USD")); err != nil {
		return nil, err
	}
	if inst.ContextUsed, err = m.Int64Gauge("ycode.llm.context_window.used",
		metric.WithUnit("tokens")); err != nil {
		return nil, err
	}
	return &inst, nil
}

// CostEstimator computes estimated cost for an API call.
type CostEstimator func(model string, inputTokens, outputTokens, cacheWrite, cacheRead int) float64

// NewInstrumentedProvider wraps a provider with OTEL instrumentation.
func NewInstrumentedProvider(inner Provider, tracer trace.Tracer, inst *APIInstruments, costFn CostEstimator) *InstrumentedProvider {
	return &InstrumentedProvider{inner: inner, tracer: tracer, inst: inst}
}

// Kind delegates to the inner provider.
func (p *InstrumentedProvider) Kind() ProviderKind {
	return p.inner.Kind()
}

// Send wraps the inner Send with an OTEL span, collecting usage metrics
// from the returned stream events.
func (p *InstrumentedProvider) Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
	ctx, span := p.tracer.Start(ctx, "ycode.api.call",
		trace.WithAttributes(
			attribute.String("llm.provider", string(p.inner.Kind())),
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.max_tokens", req.MaxTokens),
			attribute.Bool("llm.stream", req.Stream),
		),
	)
	if req.Temperature != nil {
		span.SetAttributes(attribute.Float64("llm.temperature", *req.Temperature))
	}

	innerEvents, innerErrc := p.inner.Send(ctx, req)

	outEvents := make(chan *StreamEvent, cap(innerEvents))
	outErrc := make(chan error, 1)

	go func() {
		defer close(outEvents)
		defer close(outErrc)
		defer span.End()

		start := time.Now()
		var inputTokens, outputTokens, cacheCreate, cacheRead int

		for ev := range innerEvents {
			// Capture usage from events.
			if ev.Type == "message_start" && ev.Message != nil {
				inputTokens = ev.Message.Usage.InputTokens + ev.Message.Usage.PromptTokens
				cacheCreate = ev.Message.Usage.CacheCreationInput
				cacheRead = ev.Message.Usage.CacheReadInput
			}
			if ev.Type == "message_delta" && ev.Usage != nil {
				outputTokens = ev.Usage.OutputTokens + ev.Usage.CompletionTokens
			}
			outEvents <- ev
		}

		duration := time.Since(start)

		// Set span attributes from collected usage.
		span.SetAttributes(
			attribute.Int("llm.tokens.input", inputTokens),
			attribute.Int("llm.tokens.output", outputTokens),
			attribute.Int("llm.tokens.total", inputTokens+outputTokens),
			attribute.Int("llm.tokens.cache_creation", cacheCreate),
			attribute.Int("llm.tokens.cache_read", cacheRead),
			attribute.Float64("llm.duration_ms", float64(duration.Milliseconds())),
		)

		// Forward error.
		if err := <-innerErrc; err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(
				attribute.Bool("llm.success", false),
				attribute.String("llm.error", err.Error()),
			)
			outErrc <- err
		} else {
			span.SetAttributes(attribute.Bool("llm.success", true))
		}

		// Record metrics.
		attrs := metric.WithAttributes(
			attribute.String("llm.provider", string(p.inner.Kind())),
			attribute.String("llm.model", req.Model),
		)
		p.inst.CallDuration.Record(ctx, float64(duration.Milliseconds()), attrs)
		p.inst.CallTotal.Add(ctx, 1, attrs)
		if inputTokens > 0 {
			p.inst.TokensInput.Add(ctx, int64(inputTokens), attrs)
		}
		if outputTokens > 0 {
			p.inst.TokensOutput.Add(ctx, int64(outputTokens), attrs)
		}
		if cacheRead > 0 {
			p.inst.CacheRead.Add(ctx, int64(cacheRead), attrs)
		}
		if cacheCreate > 0 {
			p.inst.CacheWrite.Add(ctx, int64(cacheCreate), attrs)
		}
		p.inst.ContextUsed.Record(ctx, int64(inputTokens+outputTokens), attrs)
	}()

	return outEvents, outErrc
}
