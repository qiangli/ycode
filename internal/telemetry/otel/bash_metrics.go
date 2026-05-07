package otel

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	bashInstrumentsOnce sync.Once
	bashExecCounter     metric.Int64Counter
	bashExecDuration    metric.Float64Histogram
)

func ensureBashInstruments() {
	bashInstrumentsOnce.Do(func() {
		m := otel.Meter("ycode.bash")
		bashExecCounter, _ = m.Int64Counter(
			"ycode.bash.exec.total",
			metric.WithDescription("Bash command invocations"),
		)
		bashExecDuration, _ = m.Float64Histogram(
			"ycode.bash.exec.duration",
			metric.WithUnit("ms"),
			metric.WithDescription("Bash command latency"),
		)
	})
}

// RecordBashExec emits the bash exec counter + duration histogram for a
// completed command. Lives in this package (not internal/runtime/bash)
// so the bash package can avoid pulling in OTel directly via a tight
// helper signature — no struct types crossing the boundary.
//
// Attributes are deliberately coarse to keep cardinality bounded:
// `success` and `background` booleans plus `exit_code` (small integer).
// The actual command text is NOT carried as an attribute; it's already
// captured in the per-tool span's input summary.
func RecordBashExec(ctx context.Context, dur time.Duration, exitCode int, success, timedOut, background bool) {
	ensureBashInstruments()
	attrs := []attribute.KeyValue{
		attribute.Bool("success", success),
		attribute.Bool("background", background),
		attribute.Int("exit_code", exitCode),
	}
	if timedOut {
		attrs = append(attrs, attribute.Bool("timed_out", true))
	}
	if bashExecCounter != nil {
		bashExecCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if bashExecDuration != nil {
		bashExecDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attrs...))
	}
}
