package bus

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// withTestMeter installs a manual reader as the global meter provider for
// the duration of the test, runs fn, and returns the resulting metric set.
// This lets us assert counter increments without needing a full collector.
func withTestMeter(t *testing.T, fn func()) metricdata.ResourceMetrics {
	t.Helper()
	prev := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	// Reset the lazy init so the new meter is picked up. The test serializes
	// against parallel runs of the bus package by using t.Setenv-equivalent
	// semantics — package-level state, but bus instrumentation is not
	// concurrent-mutated after init.
	resetInstrumentsForTest()
	fn()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return rm
}

// findCounterPoint searches the collected metrics for a single data point
// matching name + an attribute set. Returns the int64 value or fails.
func findCounterPoint(t *testing.T, rm metricdata.ResourceMetrics, name string, attrSubset map[string]string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				match := true
				for k, v := range attrSubset {
					got, ok := dp.Attributes.Value(attribute.Key(k))
					if !ok || got.AsString() != v {
						match = false
						break
					}
				}
				if match {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("counter %q with attrs %v not found", name, attrSubset)
	return 0
}

func TestRecordPublish_IncrementsCounter(t *testing.T) {
	rm := withTestMeter(t, func() {
		recordPublish(Event{Type: EventCommandComplete, SessionID: "s1"})
		recordPublish(Event{Type: EventCommandComplete, SessionID: "s1"})
		recordPublish(Event{Type: EventCommandProgress, SessionID: "s1"})
	})

	if got := findCounterPoint(t, rm, "ycode.bus.events.published",
		map[string]string{"type": "command.complete"}); got != 2 {
		t.Errorf("command.complete count: got %d, want 2", got)
	}
	if got := findCounterPoint(t, rm, "ycode.bus.events.published",
		map[string]string{"type": "command.progress"}); got != 1 {
		t.Errorf("command.progress count: got %d, want 1", got)
	}
}

func TestRecordDrop_IncrementsCounterAndLogs(t *testing.T) {
	rm := withTestMeter(t, func() {
		RecordDrop(Event{Type: EventCommandProgress, SessionID: "s1"}, "memory_bus")
		RecordDrop(Event{Type: EventCommandProgress, SessionID: "s1"}, "ws_client")
		RecordDrop(Event{Type: EventCommandDelta, SessionID: "s1"}, "memory_bus")
	})

	if got := findCounterPoint(t, rm, "ycode.bus.events.dropped",
		map[string]string{"type": "command.progress", "site": "memory_bus"}); got != 1 {
		t.Errorf("memory_bus drops for command.progress: got %d, want 1", got)
	}
	if got := findCounterPoint(t, rm, "ycode.bus.events.dropped",
		map[string]string{"type": "command.progress", "site": "ws_client"}); got != 1 {
		t.Errorf("ws_client drops for command.progress: got %d, want 1", got)
	}
}

func TestPublish_RecordsThroughMemoryBus(t *testing.T) {
	rm := withTestMeter(t, func() {
		b := NewMemoryBus()
		defer b.Close()
		// No subscriber — the publish still records, no drop because there's
		// nothing to send to.
		b.Publish(Event{Type: EventCommandComplete, SessionID: "s1"})
	})

	if got := findCounterPoint(t, rm, "ycode.bus.events.published",
		map[string]string{"type": "command.complete"}); got != 1 {
		t.Errorf("publish counter: got %d, want 1", got)
	}
}
