package bus

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Bus instrumentation. We resolve the counters from the global meter
// provider on every call rather than caching them. ycode's startup
// calls otel.SetMeterProvider twice — once with file-only exporters,
// then again after TryConnectCollector wires the gRPC pipeline to the
// embedded OTel collector. The global package's instrument delegation
// only binds the first time, so a cached counter would stay attached
// to the file-only provider and never reach Prometheus. Resolving
// per call sidesteps that entirely; the SDK caches Meter and Counter
// instances internally by name, so the steady-state cost is two map
// lookups + a mutex acquire — well under 100ns per event, which is
// invisible at our event rate.

const (
	meterName     = "ycode.bus"
	publishedName = "ycode.bus.events.published"
	droppedName   = "ycode.bus.events.dropped"
)

func publishedCounter() metric.Int64Counter {
	c, _ := otel.Meter(meterName).Int64Counter(publishedName,
		metric.WithDescription("Bus events published, by event type"))
	return c
}

func droppedCounter() metric.Int64Counter {
	c, _ := otel.Meter(meterName).Int64Counter(droppedName,
		metric.WithDescription("Bus events dropped due to slow consumer, by event type and drop site"))
	return c
}

// recordPublish increments the published-events counter for an event.
// Safe to call from any goroutine; no-op if instrumentation init failed.
func recordPublish(ev Event) {
	c := publishedCounter()
	if c == nil {
		return
	}
	c.Add(context.Background(), 1, metric.WithAttributes(
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
	if c := droppedCounter(); c != nil {
		c.Add(context.Background(), 1, metric.WithAttributes(
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
