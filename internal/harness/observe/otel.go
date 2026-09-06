// Package observe adapts the harness execution seams to OpenTelemetry.
package observe

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Observer struct {
	enabled       bool
	recordContent bool
	tracer        trace.Tracer
	attributes    []attribute.KeyValue
}

func New(config spec.Observability, tracer trace.Tracer) *Observer {
	keys := make([]string, 0, len(config.Attributes))
	for key := range config.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attrs := make([]attribute.KeyValue, 0, len(keys)+1)
	for _, key := range keys {
		attrs = append(attrs, attribute.String(key, config.Attributes[key]))
	}
	attrs = append(attrs, attribute.Bool("ycode.record_content", config.RecordContent))
	return &Observer{enabled: config.Enabled && tracer != nil, recordContent: config.RecordContent, tracer: tracer, attributes: attrs}
}

func (o *Observer) Start(ctx context.Context, operation pipeline.Operation) (context.Context, func(pipeline.Outcome)) {
	if o == nil || !o.enabled {
		return ctx, func(pipeline.Outcome) {}
	}
	attrs := append([]attribute.KeyValue(nil), o.attributes...)
	attrs = append(attrs,
		attribute.String("ycode.operation.kind", operation.Kind),
		attribute.String("ycode.operation.name", operation.Name),
	)
	if operation.Pipeline != "" {
		attrs = append(attrs, attribute.String("ycode.pipeline.name", operation.Pipeline))
	}
	if operation.Stage != "" {
		attrs = append(attrs, attribute.String("ycode.stage.type", operation.Stage))
		attrs = append(attrs, semanticAttributes(operation.Stage)...)
	}
	name := "ycode " + operation.Kind + " " + operation.Name
	ctx, span := o.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(outcome pipeline.Outcome) {
		span.SetAttributes(attribute.String("ycode.outcome.class", outcome.Class), attribute.String("error.type", outcome.Code))
		if outcome.Err != nil {
			span.RecordError(outcome.Err)
			span.SetStatus(codes.Error, outcome.Code)
		} else if outcome.Class == pipeline.OutcomeFailed || outcome.Class == pipeline.OutcomeCancelled {
			span.SetStatus(codes.Error, outcome.Code)
		} else {
			span.SetStatus(codes.Ok, outcome.Class)
		}
		span.End()
	}
}

func semanticAttributes(stage string) []attribute.KeyValue {
	switch {
	case stage == "llm.call":
		return []attribute.KeyValue{attribute.String("gen_ai.operation.name", "chat")}
	case strings.HasPrefix(stage, "bashy."):
		return []attribute.KeyValue{attribute.String("gen_ai.operation.name", "execute_tool"), attribute.String("gen_ai.tool.name", "bashy")}
	case stage == "agent.invoke":
		return []attribute.KeyValue{attribute.String("gen_ai.operation.name", "invoke_agent")}
	case stage == "hitl.review":
		return []attribute.KeyValue{attribute.String("gen_ai.operation.name", "human_review")}
	default:
		return []attribute.KeyValue{attribute.String("gen_ai.operation.name", fmt.Sprintf("execute_%s", strings.ReplaceAll(stage, ".", "_")))}
	}
}

// RecordContent reports the compiled content-recording choice to model and
// tool stage adapters without exposing a second mutable configuration seam.
func (o *Observer) RecordContent() bool { return o != nil && o.recordContent }
