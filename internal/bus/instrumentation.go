package bus

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Per-package OTel instruments for the bus. Created lazily on first use so
// the bus package has no init-order dependency on the OTel SDK setup. When
// no exporter is wired, otel.Meter returns a no-op meter and the counters
// are silent — but the slog.Debug records still land in the log stream so
// post-hoc diagnosis (e.g., "did the server publish command.complete?")
// works even without a collector.
var (
	instrOnce  sync.Once
	publishedC metric.Int64Counter
	droppedC   metric.Int64Counter
)

func initInstruments() {
	m := otel.Meter("ycode.bus")
	publishedC, _ = m.Int64Counter("ycode.bus.events.published",
		metric.WithDescription("Bus events published, by event type"))
	droppedC, _ = m.Int64Counter("ycode.bus.events.dropped",
		metric.WithDescription("Bus events dropped due to slow consumer, by event type and drop site"))
}

// recordPublish increments the published-events counter for an event.
// Safe to call from any goroutine; no-op if instrumentation init failed.
func recordPublish(ev Event) {
	instrOnce.Do(initInstruments)
	if publishedC == nil {
		return
	}
	publishedC.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("type", string(ev.Type)),
	))
}

// RecordDrop increments the dropped-events counter and emits a debug log
// line. Exposed so subscriber-side fan-out (e.g. internal/client.WSClient)
// can record drops too — the bus itself can only see drops that happen at
// its own subscriber send.
//
// site identifies the drop point: "memory_bus" (server-side fan-out),
// "ws_client" (client-side broadcast to TUI subscribers), etc. Filterable
// in metrics queries.
func RecordDrop(ev Event, site string) {
	instrOnce.Do(initInstruments)
	if droppedC != nil {
		droppedC.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("type", string(ev.Type)),
			attribute.String("site", site),
		))
	}
	slog.Debug("bus.event.dropped",
		"type", ev.Type,
		"session", ev.SessionID,
		"site", site,
	)
}

// resetInstrumentsForTest re-runs the lazy init so a test can swap in a
// fresh meter provider and observe its own counter increments. Not for
// production use — there is no reason to reset instruments at runtime.
func resetInstrumentsForTest() {
	instrOnce = sync.Once{}
	publishedC = nil
	droppedC = nil
}
