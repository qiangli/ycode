package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestOpenAICompatClient_TraceparentInjected confirms the otelhttp
// transport stamps a W3C traceparent header on every outbound chat
// request when a span is active in the caller context. This is the
// ycode-side of the cross-process trace fabric — without this hop,
// cloudbox-hub has no parent span to attach to and the end-to-end
// trace breaks at the very first boundary.
func TestOpenAICompatClient_TraceparentInjected(t *testing.T) {
	// Use an in-process tracer provider so the test stays hermetic and
	// doesn't depend on cmd/ycode/otel.go bootstrap order. The same
	// W3C propagator is installed globally by the production provider
	// in internal/telemetry/otel/provider.go.
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		// Minimal non-stream OpenAI chat response — enough for Send to
		// return cleanly so we don't trip retry/timeout logic.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	client := NewOpenAICompatClient("k", srv.URL)

	ctx, span := tp.Tracer("test").Start(context.Background(), "outer")
	defer span.End()

	events, errc := client.Send(ctx, &Request{Model: "anything", Stream: false})
	for range events { // drain
	}
	if err := <-errc; err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotTraceparent == "" {
		t.Fatal("expected outbound request to carry a traceparent header; got none")
	}
	// W3C format: 00-<32-hex trace-id>-<16-hex span-id>-<2-hex flags>.
	// We don't assert the trace-id matches the test span exactly
	// because otelhttp creates a child span and uses its IDs — what
	// matters for the framework is that *something* propagates, so
	// downstream services can attach.
	if !strings.HasPrefix(gotTraceparent, "00-") {
		t.Errorf("traceparent %q: missing W3C version prefix", gotTraceparent)
	}
	if parts := strings.Split(gotTraceparent, "-"); len(parts) != 4 {
		t.Errorf("traceparent %q: want 4 dash-separated parts, got %d", gotTraceparent, len(parts))
	}
}

// TestCloudboxLister_TraceparentInjected confirms the model picker
// path also propagates trace context. Important because dev-time
// debugging of "why is /models slow" goes through the same code.
func TestCloudboxLister_TraceparentInjected(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	// Pass nil http.Client so NewCloudboxLister builds the default
	// otelhttp-wrapped one — that's the production path.
	lister := NewCloudboxLister(srv.URL+"/v1", "tok", nil)

	ctx, span := tp.Tracer("test").Start(context.Background(), "outer")
	defer span.End()
	_ = lister(ctx)

	if gotTraceparent == "" {
		t.Fatal("expected /models request to carry a traceparent header; got none")
	}
}
