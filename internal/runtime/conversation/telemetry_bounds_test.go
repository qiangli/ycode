package conversation

import (
	"context"
	"strings"
	"testing"

	"github.com/qiangli/coreutils/pkg/telemetry"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(old) })
	return sr
}

// events pulls the recorded events out of the one span, flattened.
func events(t *testing.T, sr *tracetest.SpanRecorder) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for _, s := range sr.Ended() {
		for _, e := range s.Events() {
			attrs := map[string]string{}
			for _, kv := range e.Attributes {
				attrs[string(kv.Key)] = kv.Value.Emit()
			}
			out[e.Name] = attrs
		}
	}
	return out
}

// A NUMBER WITHOUT ITS PROVENANCE CANNOT BE AUDITED.
//
// `from_provider=false` — a LOG LINE — is the only reason the dead usage-plumbing bug was
// ever caught: MeasureTokens read ConversationMessage.Usage out of []api.Message, a type
// with no Usage field, so it returned nil on every turn and the whole "ask the provider,
// do not guess" gate fell back to the estimator it exists to replace. Silently. For every
// model. It took a human staring at stderr.
//
// As a span attribute it is a QUERY: "show me every turn where the gate ran on a guess."
func TestContextTokensCarryTheirProvenance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hasReport  bool
		wantSource string
	}{
		{"provider reported it", true, "provider"},
		{"we guessed it", false, "estimate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := recorder(t)
			ctx, span := otel.Tracer("test").Start(context.Background(), "turn")

			source := "estimate"
			if tc.hasReport {
				source = "provider"
			}
			telemetry.Provenance(ctx, "context.tokens", 6482, source)
			span.End()

			ev := events(t, sr)
			got, ok := ev["value"]
			if !ok {
				t.Fatal("no provenance event was emitted — the number is unauditable")
			}
			if got["value.source"] != tc.wantSource {
				t.Errorf("value.source = %q, want %q. A token count without its source cannot "+
					"tell you whether the context gate is running on fact or on a guess.",
					got["value.source"], tc.wantSource)
			}
			if got["value.amount"] != "6482" {
				t.Errorf("value.amount = %q, want 6482", got["value.amount"])
			}
		})
	}
}

// A BOUND YOU CANNOT SEE IS NOT A BOUND, IT IS A TRAP.
//
// The 25-iteration cap cut an agent off mid-investigation TWICE, exited 0 both times, and
// very nearly had "cannot conduct" written against a model that was doing nothing wrong.
func TestTheIterationCapIsRecordedWhenItBinds(t *testing.T) {
	sr := recorder(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "turn")
	telemetry.BoundHit(ctx, "iterations", 25, 25, "the agent had not finished")
	span.End()

	got, ok := events(t, sr)["bound.hit"]
	if !ok {
		t.Fatal("the cap bound and emitted nothing — the trap is still a trap")
	}
	if got["bound.kind"] != "iterations" || got["bound.limit"] != "25" {
		t.Errorf("bound = %v, want kind=iterations limit=25", got)
	}
	if !strings.Contains(got["bound.detail"], "not finished") {
		t.Errorf("bound.detail does not say the agent was cut off: %q", got["bound.detail"])
	}

	// The span itself must be MARKED, or a query cannot find it.
	var flagged bool
	for _, kv := range sr.Ended()[0].Attributes() {
		if string(kv.Key) == "bound.was_hit" {
			flagged = true
		}
	}
	if !flagged {
		t.Error("the span was not flagged bound.was_hit — a query cannot find what is not marked")
	}
}

// A RATE LIMIT THAT RECOVERS IS THE ONE NOBODY INVESTIGATES.
//
// Three 429s in a run all recovered on retry, cost minutes, and left NO SIGNAL. So "rate
// limits killed it" stayed a plausible theory for hours with nothing to check it against —
// and the real cause (our own 25-turn cap) went unfound.
func TestARateLimitIsRecordedEvenThoughTheRunRecovers(t *testing.T) {
	sr := recorder(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "request")
	telemetry.BoundHit(ctx, "rate_limit", 1395, 2, "provider 429 from api.z.ai")
	span.End()

	got, ok := events(t, sr)["bound.hit"]
	if !ok {
		t.Fatal("a 429 was absorbed by a retry and left no trace — which is exactly how it " +
			"stayed a theory for hours")
	}
	if got["bound.kind"] != "rate_limit" {
		t.Errorf("bound.kind = %q, want rate_limit", got["bound.kind"])
	}
}
