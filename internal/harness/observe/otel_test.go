package observe

import (
	"context"
	"errors"
	"testing"

	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObserverUsesCompiledConfigAndGenAIAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	observer := New(spec.Observability{Enabled: true, RecordContent: false, Attributes: map[string]string{"service.name": "ycode-test"}}, provider.Tracer("test"))
	_, finish := observer.Start(context.Background(), pipeline.Operation{Kind: "stage", Name: "execute", Stage: "bashy.execute"})
	finish(pipeline.Outcome{Class: pipeline.OutcomeObserved, Code: "exit-nonzero"})
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	attrs := map[string]any{}
	for _, attr := range spans[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}
	if attrs["gen_ai.tool.name"] != "bashy" || attrs["gen_ai.operation.name"] != "execute_tool" {
		t.Fatalf("attributes = %#v", attrs)
	}
	if attrs["ycode.record_content"] != false || observer.RecordContent() {
		t.Fatalf("content recording = %#v", attrs["ycode.record_content"])
	}
	for key := range attrs {
		if key == "gen_ai.input.messages" || key == "gen_ai.output.messages" {
			t.Fatalf("content attribute %q recorded", key)
		}
	}
}

func TestObserverRecordsFailuresAndDisabledIsNoop(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	observer := New(spec.Observability{Enabled: true}, provider.Tracer("test"))
	_, finish := observer.Start(context.Background(), pipeline.Operation{Kind: "stage", Name: "model", Stage: "llm.call"})
	finish(pipeline.Failure("provider", true, errors.New("down")))
	if got := recorder.Ended(); len(got) != 1 || got[0].Status().Code != codesError() {
		t.Fatalf("spans = %#v", got)
	}
	disabled := New(spec.Observability{Enabled: false}, provider.Tracer("test"))
	_, done := disabled.Start(context.Background(), pipeline.Operation{Kind: "pipeline", Name: "turn"})
	done(pipeline.Success(nil))
	if len(recorder.Ended()) != 1 {
		t.Fatal("disabled observer emitted a span")
	}
}

func TestPipelineRunnerEmitsPipelineAndStageSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	observer := New(spec.Observability{Enabled: true}, provider.Tracer("test"))
	registry := pipeline.NewRegistry()
	if err := registry.Register(pipeline.Definition{Name: "llm.call", Handler: func(context.Context, pipeline.Invocation) pipeline.Outcome { return pipeline.Success(nil) }}); err != nil {
		t.Fatal(err)
	}
	pipelines := map[string]spec.Pipeline{"turn": {Concurrency: 1, Nodes: []spec.Stage{{ID: "model", Run: spec.Run{Stage: "llm.call"}}}}}
	runner := pipeline.NewRunner(registry).WithPipelines(pipelines).WithObserver(observer)
	if err := runner.RunPipeline(context.Background(), "turn", pipeline.NewState()); err != nil {
		t.Fatal(err)
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("spans = %d", len(spans))
	}
	names := map[string]bool{}
	for _, span := range spans {
		names[span.Name()] = true
	}
	if !names["ycode stage model"] || !names["ycode pipeline turn"] {
		t.Fatalf("span names = %#v", names)
	}
}

func codesError() codes.Code { return codes.Error }
