package mesh

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TracedAgent wraps a MeshAgent with OTEL tracing.
type TracedAgent struct {
	inner  MeshAgent
	tracer trace.Tracer
	logger *slog.Logger
}

// NewTracedAgent wraps an agent with tracing.
func NewTracedAgent(inner MeshAgent) *TracedAgent {
	return &TracedAgent{
		inner:  inner,
		tracer: otel.Tracer("ycode.mesh"),
		logger: slog.Default(),
	}
}

func (ta *TracedAgent) Name() string  { return ta.inner.Name() }
func (ta *TracedAgent) Healthy() bool { return ta.inner.Healthy() }
func (ta *TracedAgent) Stop()         { ta.inner.Stop() }

func (ta *TracedAgent) Start(ctx context.Context) error {
	_, span := ta.tracer.Start(ctx, "mesh.agent.start",
		trace.WithAttributes(
			attribute.String("mesh.agent", ta.inner.Name()),
			attribute.String("mesh.timestamp", time.Now().Format(time.RFC3339)),
		))
	defer span.End()

	ta.logger.Info("mesh.agent.starting", "agent", ta.inner.Name())
	err := ta.inner.Start(ctx)
	if err != nil {
		span.RecordError(err)
	}
	return err
}
