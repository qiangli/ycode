package wrap

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"google.golang.org/grpc/grpclog"
)

// loggerInitialized is flipped by initLoggerFromEnv after a real
// TextHandler is installed on slog.Default(). installOTelLogBridge
// must refuse to wrap the default until then — slog's package-level
// defaultHandler routes through the standard log package, which loops
// back into slog.Default(), so a teeLogHandler whose primary is the
// defaultHandler will infinitely recurse on the first log line. Tests
// that exercise setupOTel in isolation deliberately do not call
// initLoggerFromEnv (it would touch user-global slog state), so this
// guard is what keeps them safe.
var loggerInitialized atomic.Bool

// initLoggerFromEnv sets slog.Default() and grpc-go's internal logger
// to write to ~/.agents/ycode/observability/wrap.log (append mode) at
// the level declared by YCODE_LOG_LEVEL (default INFO). Called from
// both wrap.Run (the parent CLI path) and ShimMain (the child shim
// path).
//
// Wrap-mode discipline: nothing diagnostic reaches stderr. The wrapped
// TUI owns the terminal, and slog INFO lines plus grpc-go's
// "addrConn.createTransport failed" warnings (emitted when the OTLP
// gRPC dial cannot reach a `ycode serve` collector) would otherwise
// corrupt the alt-screen. Hard-error notices that the operator must
// see still go through dedicated fmt.Fprintf(os.Stderr, ...) calls in
// wrap.Run / ShimMain — those are intentional and stay.
//
// Returns a non-nil close func the caller defers to release the file
// handle. Falls back to io.Discard (and a no-op close) when the log
// file cannot be opened, preserving the no-stderr-noise invariant.
func initLoggerFromEnv() (close func()) {
	lvl := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("YCODE_LOG_LEVEL"))) {
	case "debug", "trace":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}

	var out io.Writer = io.Discard
	closeFn := func() {}
	if logPath, err := wrapLogPath(); err == nil {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			out = f
			closeFn = func() { _ = f.Close() }
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})))
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(out, out, out))
	loggerInitialized.Store(true)

	return closeFn
}

// installOTelLogBridge tees slog.Default() into the OTel log pipeline
// so wrap's diagnostics flow to the file exporter (always, since
// setupOTel sets PersistLogs=true) and to VictoriaLogs (when a
// reachable collector was detected at NewProvider time). Stacks on top
// of the file-text handler initLoggerFromEnv installed — the text
// handler keeps a human-readable trail in wrap.log even when the OTel
// pipeline is in flux.
//
// Uses the global otel log.LoggerProvider that setupOTel published via
// otellog.SetLoggerProvider, so callers don't need to thread a
// provider through. No-op when initLoggerFromEnv hasn't installed a
// safe primary handler yet (see loggerInitialized above) or when no
// LoggerProvider is set (ExportOff path).
func installOTelLogBridge() {
	if !loggerInitialized.Load() {
		return
	}
	current := slog.Default().Handler()
	otelHandler := otelslog.NewHandler("ycode.wrap")
	slog.SetDefault(slog.New(&teeLogHandler{primary: current, secondary: otelHandler}))
}

// teeLogHandler forwards records to two handlers. Mirrors the
// cmd/ycode/otel.go variant; duplicated here to keep wrap's import
// graph free of cmd/ycode (which would pull in the whole CLI).
type teeLogHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (h *teeLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level) || h.secondary.Enabled(ctx, level)
}

func (h *teeLogHandler) Handle(ctx context.Context, record slog.Record) error {
	_ = h.secondary.Handle(ctx, record)
	return h.primary.Handle(ctx, record)
}

func (h *teeLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeLogHandler{
		primary:   h.primary.WithAttrs(attrs),
		secondary: h.secondary.WithAttrs(attrs),
	}
}

func (h *teeLogHandler) WithGroup(name string) slog.Handler {
	return &teeLogHandler{
		primary:   h.primary.WithGroup(name),
		secondary: h.secondary.WithGroup(name),
	}
}

func wrapLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".agents", "ycode", "observability")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "wrap.log"), nil
}
