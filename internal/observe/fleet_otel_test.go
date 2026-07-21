package observe

// The Fix-3 gate, verified against in-process OTEL SDK receivers: a cascade run
// emits one agent.turn span per turn carrying the served model and token/cost
// attributes (escalated=true exactly on the switch turn), and the fleet.*
// metrics aggregate tokens, spend, and the base→premium escalation count.
//
// NOTE on ordering: fleet metric instruments are built once per process against
// the GLOBAL meter provider (see metrics.go). TestFleetMetrics must therefore
// install its MeterProvider before anything in this package flushes a turn —
// which it guarantees by being the first test in the alphabetically-first test
// file. Keep it that way.

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestFleetMetrics: fleet.tokens + fleet.cost_usd accumulate by model, and
// fleet.escalation counts the base→premium SWITCH exactly once — not once per
// premium turn.
func TestFleetMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	runCascade(t, Options{})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	metrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			metrics[m.Name] = m
		}
	}

	// fleet.tokens, summed per model+kind. The cascade ran glm-4.6 for two
	// turns (1000+1500 prompt, 200+300 completion) and claude-opus-4-8 for one
	// (2000 prompt, 500 completion).
	tokens, ok := metrics[MetricTokens].Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s missing or wrong type: %+v", MetricTokens, metrics[MetricTokens])
	}
	tokenSum := map[string]int64{}
	for _, dp := range tokens.DataPoints {
		model, _ := dp.Attributes.Value("model")
		kind, _ := dp.Attributes.Value("kind")
		tokenSum[model.AsString()+"/"+kind.AsString()] += dp.Value
	}
	for key, want := range map[string]int64{
		"glm-4.6/prompt":             2500,
		"glm-4.6/completion":         500,
		"claude-opus-4-8/prompt":     2000,
		"claude-opus-4-8/completion": 500,
	} {
		if tokenSum[key] != want {
			t.Errorf("fleet.tokens[%s] = %d, want %d (all: %v)", key, tokenSum[key], want, tokenSum)
		}
	}

	// fleet.cost_usd per model, priced by the flat $1/M-in $2/M-out test table.
	cost, ok := metrics[MetricCost].Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("%s missing or wrong type", MetricCost)
	}
	costSum := map[string]float64{}
	for _, dp := range cost.DataPoints {
		model, _ := dp.Attributes.Value("model")
		costSum[model.AsString()] += dp.Value
	}
	if !floatEq(costSum["glm-4.6"], 0.0035) || !floatEq(costSum["claude-opus-4-8"], 0.003) {
		t.Errorf("fleet.cost_usd = %v, want glm-4.6:0.0035 claude-opus-4-8:0.003", costSum)
	}

	// fleet.escalation: exactly ONE switch, labeled with the pair and why.
	escal, ok := metrics[MetricEscalation].Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s missing or wrong type", MetricEscalation)
	}
	var switches int64
	for _, dp := range escal.DataPoints {
		switches += dp.Value
		if base, _ := dp.Attributes.Value("base_model"); base.AsString() != "glm-4.6" {
			t.Errorf("escalation base_model = %v", base)
		}
		if served, _ := dp.Attributes.Value("served_model"); served.AsString() != "claude-opus-4-8" {
			t.Errorf("escalation served_model = %v", served)
		}
		if reason, _ := dp.Attributes.Value("reason"); reason.AsString() == "" {
			t.Error("escalation datapoint has no reason")
		}
	}
	if switches != 1 {
		t.Errorf("fleet.escalation = %d, want exactly 1 switch", switches)
	}
}

// TestAgentTurnSpans: one agent.turn span per turn, carrying the model, token,
// cost, and escalation attributes, with tool calls attached as span events.
func TestAgentTurnSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	runCascade(t, Options{Tracer: tp.Tracer("test")})

	var spans []sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == "agent.turn" {
			spans = append(spans, s)
		}
	}
	if len(spans) != 3 {
		t.Fatalf("agent.turn spans = %d, want one per turn (3)", len(spans))
	}

	attrs := func(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
		m := map[attribute.Key]attribute.Value{}
		for _, kv := range s.Attributes() {
			m[kv.Key] = kv.Value
		}
		return m
	}

	wantServed := []string{"glm-4.6", "glm-4.6", "claude-opus-4-8"}
	wantPrompt := []int64{1000, 1500, 2000}
	wantCompletion := []int64{200, 300, 500}
	for i, s := range spans {
		a := attrs(s)
		if got := a["served_model"].AsString(); got != wantServed[i] {
			t.Errorf("span %d served_model = %q, want %q", i, got, wantServed[i])
		}
		if got := a["base_model"].AsString(); got != "glm-4.6" {
			t.Errorf("span %d base_model = %q", i, got)
		}
		if got := a["from_provider"].AsString(); got == "" {
			t.Errorf("span %d missing from_provider", i)
		}
		if got := a["prompt_tokens"].AsInt64(); got != wantPrompt[i] {
			t.Errorf("span %d prompt_tokens = %d, want %d", i, got, wantPrompt[i])
		}
		if got := a["completion_tokens"].AsInt64(); got != wantCompletion[i] {
			t.Errorf("span %d completion_tokens = %d, want %d", i, got, wantCompletion[i])
		}
		if got := a["cost_usd"].AsFloat64(); got <= 0 {
			t.Errorf("span %d cost_usd = %v, want > 0", i, got)
		}
		wantEscalated := i == 2
		if got := a["escalated"].AsBool(); got != wantEscalated {
			t.Errorf("span %d escalated = %v, want %v", i, got, wantEscalated)
		}
	}
	if reason := attrs(spans[2])["reason"].AsString(); reason == "" {
		t.Error("escalated span carries no reason")
	}

	// Turn 0 made two tool calls; they ride the span as events.
	var toolEvents int
	for _, ev := range spans[0].Events() {
		if ev.Name == "tool.call" {
			toolEvents++
		}
	}
	if toolEvents != 2 {
		t.Errorf("span 0 tool.call events = %d, want 2", toolEvents)
	}
}
