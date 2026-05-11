package otel

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// withOriginProvider creates a provider with origin fields populated
// and an in-memory metric reader. Returns the collected metrics after
// fn runs.
func withOriginProvider(t *testing.T, fn func()) metricdata.ResourceMetrics {
	t.Helper()
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	reader := sdkmetric.NewManualReader()
	prov, err := NewProvider(context.Background(), ProviderConfig{
		ServiceName:    "ycode-test",
		ServiceVersion: "test-0.0.1",
		SessionID:      "session-abc",
		InstanceID:     "instance-xyz",
		SampleRate:     1.0,
		ProjectID:      "github.com/foo/bar",
		ProjectName:    "bar",
		ProjectRoot:    "/abs/path/to/bar",
		AgentTool:      "prompt",
		Personality:    "stern",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	// Replace the meter reader so we can collect what NewProvider
	// emits. We mount a fresh provider with the same resource and
	// our manual reader, then set it global.
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(prov.resource),
	)
	otel.SetMeterProvider(mp)

	fn()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

// resourceAttr looks up an attribute on the collected resource by
// key. Returns ("", false) if absent.
func resourceAttr(rm metricdata.ResourceMetrics, key string) (string, bool) {
	if rm.Resource == nil {
		return "", false
	}
	v, ok := rm.Resource.Set().Value(attribute.Key(key))
	if !ok {
		return "", false
	}
	return v.AsString(), true
}

func TestProvider_CarriesOriginResourceAttributes(t *testing.T) {
	rm := withOriginProvider(t, func() {
		// Fire one metric so something flushes through the resource.
		counter, _ := otel.Meter("ycode.test").Int64Counter("test.counter")
		counter.Add(context.Background(), 1)
	})

	wants := map[string]string{
		"ycode.project.id":   "github.com/foo/bar",
		"ycode.project.name": "bar",
		"ycode.project.root": "/abs/path/to/bar",
		"ycode.agent.tool":   "prompt",
		"ycode.personality":  "stern",
	}
	for k, want := range wants {
		got, ok := resourceAttr(rm, k)
		if !ok {
			t.Errorf("resource attr %s missing", k)
			continue
		}
		if got != want {
			t.Errorf("resource attr %s = %q; want %q", k, got, want)
		}
	}
}

func TestProvider_OmitsEmptyOriginFields(t *testing.T) {
	// Build a provider with only ProjectName set; the other origin
	// fields should not appear as resource attributes.
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	reader := sdkmetric.NewManualReader()
	prov, err := NewProvider(context.Background(), ProviderConfig{
		ServiceName: "ycode-test",
		SessionID:   "s1",
		ProjectName: "only-this",
	})
	if err != nil {
		t.Fatal(err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(prov.resource),
	)
	otel.SetMeterProvider(mp)
	counter, _ := otel.Meter("ycode.test").Int64Counter("test.counter")
	counter.Add(context.Background(), 1)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	if got, _ := resourceAttr(rm, "ycode.project.name"); got != "only-this" {
		t.Errorf("project.name = %q", got)
	}
	for _, key := range []string{"ycode.project.id", "ycode.project.root", "ycode.agent.tool", "ycode.personality"} {
		if _, ok := resourceAttr(rm, key); ok {
			t.Errorf("resource attr %s should be absent when ProviderConfig field is empty", key)
		}
	}
}

// fakeToolFunc returns a deterministic response so the middleware
// test doesn't depend on real tool behavior.
func fakeToolFunc(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

// fakeInstruments builds the minimum Instruments handles needed by
// ToolMiddleware so we can run it in tests against an in-memory
// reader.
func fakeInstruments(t *testing.T, m attribute.KeyValue) *Instruments {
	t.Helper()
	meter := otel.Meter("ycode.test.tools")
	dur, err := meter.Float64Histogram("ycode.tool.call.duration")
	if err != nil {
		t.Fatal(err)
	}
	total, err := meter.Int64Counter("ycode.tool.call.total")
	if err != nil {
		t.Fatal(err)
	}
	return &Instruments{
		ToolCallDuration: dur,
		ToolCallTotal:    total,
	}
}

func TestToolMiddleware_AttachesAgentClientFromCtx(t *testing.T) {
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	inst := fakeInstruments(t, attribute.String("test", "true"))
	tracer := otel.Tracer("ycode.test.tools")
	mw := ToolMiddleware(tracer, inst)
	wrapped := mw("my-tool", fakeToolFunc)

	// Two calls: one with an agent.client on ctx, one without.
	ctx := mcp.WithAgentClient(context.Background(), "claude-code")
	if _, err := wrapped(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	// Expect exactly one timeseries tagged ycode.agent.client=claude-code
	// and exactly one without that label.
	withClient := 0
	withoutClient := 0
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			if met.Name != "ycode.tool.call.total" {
				continue
			}
			sum, ok := met.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				v, ok := dp.Attributes.Value(attribute.Key("ycode.agent.client"))
				if ok && v.AsString() == "claude-code" {
					withClient += int(dp.Value)
				} else {
					withoutClient += int(dp.Value)
				}
			}
		}
	}
	if withClient != 1 {
		t.Errorf("expected 1 counter point with agent.client=claude-code; got %d", withClient)
	}
	if withoutClient != 1 {
		t.Errorf("expected 1 counter point without agent.client; got %d", withoutClient)
	}
}
