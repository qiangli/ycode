package otel

import (
	"log/slog"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
)

// InstallQuietErrorHandler swaps OTel's default error handler (which
// writes via Go's stdlib `log.Print` and so leaks unstructured noise
// into stderr) for one that routes through slog. Repeated export errors
// from an unreachable collector fire every PeriodicReader tick; keep the
// first warning visible and suppress repeats so telemetry degrades quietly.
//
// Other errors (encode failures, programmer error, etc.) also route at
// slog.Warn once per class so genuine bugs remain visible without flooding
// long-running agent output.
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
		logOTELSDKErrorOnce("collector_unavailable", "otel: collector unavailable", msg)
		return
	}
	logOTELSDKErrorOnce("sdk_error", "otel: sdk error", msg)
}

var otelSDKErrorLogSuppressor = newOncePerClassLogSuppressor()

type oncePerClassLogSuppressor struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newOncePerClassLogSuppressor() *oncePerClassLogSuppressor {
	return &oncePerClassLogSuppressor{seen: make(map[string]struct{})}
}

func logOTELSDKErrorOnce(class, message, err string) {
	if !otelSDKErrorLogSuppressor.shouldLog(class) {
		return
	}
	slog.Warn(message, "error", err)
}

func (s *oncePerClassLogSuppressor) shouldLog(class string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[class]; ok {
		return false
	}
	s.seen[class] = struct{}{}
	return true
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
