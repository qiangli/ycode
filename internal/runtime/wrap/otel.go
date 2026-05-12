package wrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// ExportMode selects the wrap-parent's OTel local sink. The wrap CLI
// flag --otel-export accepts the string form of each.
type ExportMode string

const (
	// ExportFile (default) — spans persist to
	// ~/.agents/ycode/otel/instances/wrap-<pid>/ as rotating JSONL,
	// the same on-disk shape the main app uses. Operators can `cat`
	// or pipe these into pulse after the fact.
	ExportFile ExportMode = "file"

	// ExportConsole — file mode plus an additional stdouttrace
	// processor that prints each span to stderr as JSON. Verbose;
	// for debugging the wrap pipeline itself, not for everyday use.
	ExportConsole ExportMode = "console"

	// ExportOff — no provider installed at all; spans land in the
	// global no-op tracer. The existing per-exec slog.Debug line
	// remains (when YCODE_LOG_LEVEL=debug). Lowest overhead.
	ExportOff ExportMode = "off"
)

// ParseExportMode normalizes a CLI / env value to an ExportMode.
// Empty / unrecognized falls back to file (the documented default).
// The YCODE_WRAP_OTEL_EXPORT env, when set, takes precedence over
// the flag value — same escape-hatch shape as YCODE_WRAP_RUNTIME_HOOKS.
func ParseExportMode(flag string) ExportMode {
	if env := os.Getenv("YCODE_WRAP_OTEL_EXPORT"); env != "" {
		flag = env
	}
	switch ExportMode(flag) {
	case ExportFile, ExportConsole, ExportOff:
		return ExportMode(flag)
	case "":
		return ExportFile
	default:
		// Surface the user's typo at the next layer up; for now, log
		// and degrade to file mode so the wrap doesn't hard-fail.
		slog.Warn("wrap: unknown --otel-export value; falling back to file",
			"value", flag)
		return ExportFile
	}
}

// SetupOTel is the exported variant of setupOTel used by both the
// wrap parent and by `ycode internal-shell-trace` (which inherits a
// wrap session via env). The trace subprocess calls this so its
// per-shell-out parent+child spans land in the same file (and same
// collector) as the wrap parent's session span — the TRACEPARENT
// env carries trace nesting across the process boundary.
func SetupOTel(ctx context.Context, mode ExportMode, agentName, profileName string) (shutdown func()) {
	return setupOTel(ctx, mode, agentName, profileName)
}

// setupOTel installs the wrap-parent's OTel provider per the chosen
// export mode and tries to upgrade to dual-export when a ycode-serve
// is reachable via the manifest. Returns a shutdown closure the
// caller must defer to flush exporters on exit.
//
// Always returns a non-nil shutdown closure (a no-op for ExportOff
// or when provider init fails) so callers can defer unconditionally.
func setupOTel(ctx context.Context, mode ExportMode, agentName, profileName string) (shutdown func()) {
	if mode == ExportOff {
		return func() {}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("wrap: cannot locate home dir; OTel disabled", "err", err)
		return func() {}
	}
	instanceID := "wrap-" + uuid.New().String()
	dataDir := filepath.Join(home, ".agents", "ycode", "otel")
	instanceDir := filepath.Join(dataDir, "instances", instanceID)

	provider, err := yotel.NewProvider(ctx, yotel.ProviderConfig{
		ServiceName:    "ycode.wrap",
		ServiceVersion: wrapVersion(),
		SessionID:      instanceID,
		InstanceID:     instanceID,
		SampleRate:     1.0,
		DataDir:        dataDir,
		InstanceDir:    instanceDir,
		PersistTraces:  true,
		// StartExecSpan records ycode.exec.total / ycode.exec.duration
		// through the global meter, but enabling local PersistMetrics
		// here would build a file PeriodicReader that TryConnectCollector
		// then cannot share with the rebuilt MeterProvider (SDK forbids
		// binding the same Reader to two providers, and the orphaned
		// reader fails its second Shutdown with "reader is shutdown").
		// Until TryConnectCollector grows a proper provider-rebuild
		// dance (today it limps via duplicate-registration skip), wrap
		// ships exec metrics via gRPC only — fine for the canonical
		// "serve is up" path, dropped silently otherwise.
		PersistMetrics: false,
		// Wrap itself emits no OTel log records (its diagnostics go to
		// slog/stderr). Leave false until something in the wrap path
		// starts producing structured logs.
		PersistLogs: false,
		AgentTool:   "wrap",
	})
	if err != nil {
		slog.Warn("wrap: OTel provider init failed; continuing without telemetry",
			"err", err)
		return func() {}
	}

	// Console mode adds a stderr SpanProcessor on top of the file
	// processor NewProvider installed. The two run in parallel; spans
	// land on disk AND echo to stderr as JSON.
	if mode == ExportConsole {
		if exp, err := stdouttrace.New(stdouttrace.WithWriter(os.Stderr), stdouttrace.WithPrettyPrint()); err == nil {
			provider.TracerProvider.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(exp))
		} else {
			slog.Debug("wrap: stdouttrace exporter init failed", "err", err)
		}
	}

	// Dual-export upgrade: when a running `ycode serve` advertises an
	// OTLP endpoint in ~/.agents/ycode/manifest.json, push to it as
	// well. Failure is non-fatal — we stay in file-only mode and
	// surface the fallback at INFO so operators can tell which path
	// is active without enabling debug logging.
	dualExport := false
	collectorAddr := ""
	if addr, ok := ReadServeManifest(); ok {
		collectorAddr = addr
		// 2s budget keeps wrap startup snappy when serve is down or
		// hung; the SDK's per-exporter dial timeout is bounded by
		// this parent context.
		connectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		dualExport = provider.TryConnectCollector(connectCtx, addr)
		cancel()
	}

	slog.Info("wrap: OTel exporter installed",
		"mode", mode,
		"data_dir", instanceDir,
		"agent", agentName,
		"profile", profileName,
		"collector", collectorAddr,
		"dual_export", dualExport,
	)

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Provider.Shutdown now drains each provider (Tracer → Meter →
		// Logger) before closing the underlying files, so no callsite
		// ForceFlush is required. If a short-lived wrap exits before
		// the metric PeriodicReader's 15s tick, the provider's final
		// collect+export still ships the buffered batch.
		if err := provider.Shutdown(shutdownCtx); err != nil {
			slog.Debug("wrap: OTel shutdown error", "err", err)
		}
	}
}

// wrapVersion returns a best-effort version string for the OTel
// service.version resource attribute. The main `version` const lives
// in cmd/ycode/main.go and is not importable from this package; we
// fall back to a placeholder so the resource is always populated.
func wrapVersion() string {
	if v := os.Getenv("YCODE_VERSION"); v != "" {
		return v
	}
	return "wrap"
}

// formatExportModes returns a help-text friendly list of the known
// modes — used by the cobra flag's usage string in cmd/ycode/wrap.go.
func formatExportModes() string {
	return fmt.Sprintf("%s | %s | %s", ExportFile, ExportConsole, ExportOff)
}
