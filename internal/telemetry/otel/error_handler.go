package otel

import (
	"log/slog"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
)

// InstallQuietErrorHandler swaps OTel's default error handler (which
// writes via Go's stdlib `log.Print` and so leaks unstructured noise
// into stderr) for one that routes through slog. The well-known
// "collector unreachable" signatures — fired every PeriodicReader tick
// when ycode wraps a tool but no `ycode serve` is up — are demoted to
// slog.Debug so they stay invisible at the default INFO level. They
// remain inspectable via YCODE_LOG_LEVEL=debug, and the file metric
// exporter still captures the underlying samples regardless of gRPC
// health, so demoting these doesn't lose data.
//
// Other errors (encode failures, programmer error, etc.) route at
// slog.Warn so genuine bugs aren't silently swallowed.
//
// Wrapped-process stdout/stderr/exit codes are unaffected — this only
// changes where ycode's own OTel-SDK error log goes.
//
// Safe and idempotent to call from any entrypoint; the first call
// wins because otel.SetErrorHandler is global, but later calls are a
// harmless re-install of the same handler.
func InstallQuietErrorHandler() {
	installErrorHandlerOnce.Do(func() {
		otel.SetErrorHandler(quietErrorHandler{})
	})
}

var installErrorHandlerOnce sync.Once

type quietErrorHandler struct{}

func (quietErrorHandler) Handle(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if isCollectorUnavailable(msg) {
		slog.Debug("otel: collector unavailable", "error", msg)
		return
	}
	slog.Warn("otel: sdk error", "error", msg)
}

// isCollectorUnavailable matches the substrings the OTLP exporters
// emit when the collector is missing, slow, or unreachable on
// loopback. These all share the same cause: an offline collector.
// Kept as substring matching because the SDK wraps the underlying
// gRPC/HTTP errors in its own formatting and the wire-level codes
// surface only through the message body.
func isCollectorUnavailable(msg string) bool {
	if msg == "" {
		return false
	}
	switch {
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "code = Unavailable"),
		strings.Contains(msg, "exporter export timeout"),
		strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "i/o timeout"):
		return true
	}
	return false
}
