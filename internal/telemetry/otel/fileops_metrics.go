package otel

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	fileopsInstrumentsOnce sync.Once
	fileopsOpsTotal        metric.Int64Counter
	fileopsBytesTotal      metric.Int64Counter
)

func ensureFileopsInstruments() {
	fileopsInstrumentsOnce.Do(func() {
		m := otel.Meter("ycode.fileops")
		fileopsOpsTotal, _ = m.Int64Counter(
			"ycode.fileops.ops.total",
			metric.WithDescription("File-system operations executed by ycode tools, broken out by op (read/write/edit)"),
		)
		fileopsBytesTotal, _ = m.Int64Counter(
			"ycode.fileops.bytes.total",
			metric.WithUnit("By"),
			metric.WithDescription("Bytes read or written through ycode file-ops tools"),
		)
	})
}

// RecordFileop emits the fileops counter (and bytes counter when bytes
// is non-zero) for one read/write/edit operation. Pass:
//   - op:    "read" | "write" | "edit"
//   - bytes: length of payload (read result, written content); 0 if
//     not applicable
//   - success: true if the operation completed without error
//
// Counters are aggregate (over all paths) — per-path detail belongs in
// the tool-call span attributes that the tool middleware already
// captures.
func RecordFileop(ctx context.Context, op string, bytes int, success bool) {
	ensureFileopsInstruments()
	attrs := []attribute.KeyValue{
		attribute.String("op", op),
		attribute.Bool("success", success),
	}
	if fileopsOpsTotal != nil {
		fileopsOpsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if bytes > 0 && fileopsBytesTotal != nil {
		fileopsBytesTotal.Add(ctx, int64(bytes), metric.WithAttributes(attrs...))
	}
}
