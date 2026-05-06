package otel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestInstruments_InPlaceMutationRoutesToNewProvider pins the regression
// behind "no ycode_* metric panel ever populated except hostmetrics":
// TryConnectCollector swaps the global MeterProvider after startup, and
// every consumer that captured *Instruments before the swap (the
// conversation runtime, tool middleware, the diagnostic bridge) must
// observe its counter handles re-bound to the new provider's meter
// after p.Instruments is updated. If the rebind replaces the *pointer*
// rather than mutating the struct in place, those captures stay bound
// to the file-only meter and metrics never reach the gRPC exporter →
// collector → Prometheus.
//
// The test mirrors what TryConnectCollector does: build initial
// instruments from one MeterProvider, capture the pointer, then
// rebuild from a second MeterProvider and copy the struct in place.
// Increments through the captured pointer must land in the second
// provider's reader, not the first.
func TestInstruments_InPlaceMutationRoutesToNewProvider(t *testing.T) {
	r1 := sdkmetric.NewManualReader()
	mp1 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r1))
	t.Cleanup(func() { _ = mp1.Shutdown(context.Background()) })

	inst1, err := NewInstruments(mp1.Meter("ycode"))
	if err != nil {
		t.Fatalf("NewInstruments mp1: %v", err)
	}

	// Simulate a consumer captured before the swap (e.g. conversation
	// runtime's r.otel.Inst at attach time).
	captured := inst1

	// Increment via captured pointer; should land in mp1.
	captured.SessionTurns.Add(context.Background(), 1)

	// Now perform the swap analogue.
	r2 := sdkmetric.NewManualReader()
	mp2 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r2))
	t.Cleanup(func() { _ = mp2.Shutdown(context.Background()) })

	inst2, err := NewInstruments(mp2.Meter("ycode"))
	if err != nil {
		t.Fatalf("NewInstruments mp2: %v", err)
	}

	// In-place mutation: this is the production fix at provider.go:294.
	*inst1 = *inst2

	// Capture's fields now refer to instruments bound to mp2.
	// Increment via the captured pointer must land in mp2's reader.
	captured.SessionTurns.Add(context.Background(), 5)

	// Verify mp1 saw 1, mp2 saw 5.
	got1 := readCounter(t, r1, "ycode.session.turns")
	got2 := readCounter(t, r2, "ycode.session.turns")
	if got1 != 1 {
		t.Errorf("mp1 (pre-swap): got %d, want 1", got1)
	}
	if got2 != 5 {
		t.Errorf("mp2 (post-swap): got %d, want 5 — captured pointer didn't see in-place mutation", got2)
	}
}

func readCounter(t *testing.T, r sdkmetric.Reader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				var total int64
				for _, dp := range sum.DataPoints {
					total += dp.Value
				}
				return total
			}
		}
	}
	return 0
}

var _ = attribute.String // keep import for future attribute-aware assertions
