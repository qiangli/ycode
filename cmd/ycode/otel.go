package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/session"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
	"github.com/qiangli/ycode/internal/tools"
)

// otelResult holds the outputs of setupOTEL for wiring into other components.
type otelResult struct {
	shutdown func()
	convOTEL *conversation.OTELConfig
}

// resolveOTELDataDir returns the OTEL storage path using the priority:
// OTEL_STORAGE_PATH env > config dataDir > default ~/.agents/ycode/otel.
func resolveOTELDataDir(obs *config.ObservabilityConfig) string {
	if v := os.Getenv("OTEL_STORAGE_PATH"); v != "" {
		return v
	}
	if obs != nil && obs.DataDir != "" {
		return obs.DataDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents", "ycode", "otel")
}

// setupFileOTEL initializes lightweight file-only OTEL instrumentation for CLI mode.
// No collector, no gRPC — traces and metrics persist to local JSONL files.
// This is always-on so every ycode session produces queryable telemetry.
func setupFileOTEL(cfg *config.Config, sess *session.Session, toolReg *tools.Registry, provider api.Provider, opener yotel.FileOpener) *otelResult {
	dataDir := resolveOTELDataDir(cfg.Observability)
	instanceID := sess.ID
	instanceDir := filepath.Join(dataDir, "instances", instanceID)

	ctx := context.Background()
	otelProvider, err := yotel.NewProvider(ctx, yotel.ProviderConfig{
		// No CollectorAddr — file-only mode.
		ServiceName:    "ycode",
		ServiceVersion: version,
		SessionID:      sess.ID,
		InstanceID:     instanceID,
		SampleRate:     1.0,
		DataDir:        dataDir,
		InstanceDir:    instanceDir,
		PersistTraces:  true,
		PersistMetrics: true,
		Opener:         opener,
	})
	if err != nil {
		slog.Warn("otel: file-only init failed, continuing without telemetry", "error", err)
		return &otelResult{shutdown: func() {}}
	}

	// Apply tool middleware for per-tool spans.
	tracer := otelProvider.Tracer("ycode.tools")
	mw := yotel.ToolMiddleware(tracer, otelProvider.Instruments)
	for _, name := range toolReg.Names() {
		toolName := name
		if err := toolReg.ApplyMiddleware(toolName, func(next tools.ToolFunc) tools.ToolFunc {
			wrapped := mw(toolName, yotel.ToolFunc(next))
			return tools.ToolFunc(wrapped)
		}); err != nil {
			slog.Debug("otel: apply tool middleware", "tool", toolName, "error", err)
		}
	}

	providerKind := ""
	if provider != nil {
		providerKind = string(provider.Kind())
	}

	convCfg := &conversation.OTELConfig{
		Tracer:   otelProvider.Tracer("ycode.conversation"),
		Inst:     otelProvider.Instruments,
		Provider: providerKind,
	}

	// Set up request logger for conversation audit (always enabled in file mode).
	reqLogger, err := yotel.NewRequestLogger(instanceDir, yotel.RequestLoggerConfig{
		RetentionDays:  3,
		LogToolDetails: true,
		Opener:         opener,
	})
	if err != nil {
		slog.Debug("otel: request logger init failed", "error", err)
	} else {
		convCfg.ReqLogger = reqLogger
	}

	// Start retention cleanup.
	yotel.StartRetentionCleanup(ctx, dataDir, 3*24*time.Hour)

	// Try connecting to a running collector for dual-export.
	// This enables the workflow: start ycode solo, then start ycode serve,
	// and ycode auto-publishes to the shared collector.
	collectorAddr := "127.0.0.1:4317"
	if cfg.Observability != nil && cfg.Observability.CollectorAddr != "" {
		collectorAddr = cfg.Observability.CollectorAddr
	}
	if otelProvider.TryConnectCollector(ctx, collectorAddr) {
		slog.Debug("otel: dual-export mode (file + collector)", "collector", collectorAddr)
		if otelProvider.LoggerProvider != nil {
			convCfg.ConvLogger = yotel.NewConversationLogger(otelProvider.LoggerProvider, instanceID)
		}
	} else {
		slog.Debug("otel: file-only mode (no collector available)", "dataDir", dataDir)
	}

	return &otelResult{
		shutdown: func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := otelProvider.Shutdown(shutdownCtx); err != nil {
				slog.Debug("otel: shutdown error", "error", err)
			}
		},
		convOTEL: convCfg,
	}
}

// setupOTEL initializes full OTEL instrumentation with gRPC export to collector.
// It is non-blocking — if initialization fails, it logs a warning and returns a no-op.
func setupOTEL(cfg *config.Config, sess *session.Session, toolReg *tools.Registry, provider api.Provider, opener yotel.FileOpener) *otelResult {
	obs := cfg.Observability

	dataDir := resolveOTELDataDir(obs)

	collectorAddr := obs.CollectorAddr
	if collectorAddr == "" {
		// Use the embedded collector's gRPC port (4317 by default).
		collectorAddr = "127.0.0.1:4317"
	}

	sampleRate := obs.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	// Create OTEL provider.
	ctx := context.Background()
	instanceID := sess.ID // Session ID is the instance identifier — unique per process.
	instanceDir := filepath.Join(dataDir, "instances", instanceID)

	otelProvider, err := yotel.NewProvider(ctx, yotel.ProviderConfig{
		CollectorAddr:  collectorAddr,
		ServiceName:    "ycode",
		ServiceVersion: version,
		SessionID:      sess.ID,
		InstanceID:     instanceID,
		SampleRate:     sampleRate,
		DataDir:        dataDir,
		InstanceDir:    instanceDir,
		PersistTraces:  obs.PersistTraces,
		PersistMetrics: obs.PersistMetrics,
		Opener:         opener,
	})
	if err != nil {
		slog.Warn("otel: init failed, continuing without telemetry", "error", err)
		return &otelResult{shutdown: func() {}}
	}

	// Create OTELSink and wire it into existing telemetry pipelines.
	_ = yotel.NewOTELSink(otelProvider)

	// Apply OTEL tool middleware (captures full input/output for self-healing).
	tracer := otelProvider.Tracer("ycode.tools")
	mw := yotel.ToolMiddleware(tracer, otelProvider.Instruments)
	for _, name := range toolReg.Names() {
		toolName := name
		if err := toolReg.ApplyMiddleware(toolName, func(next tools.ToolFunc) tools.ToolFunc {
			wrapped := mw(toolName, yotel.ToolFunc(next))
			return tools.ToolFunc(wrapped)
		}); err != nil {
			slog.Debug("otel: apply tool middleware", "tool", toolName, "error", err)
		}
	}

	// Build conversation OTEL config for wiring into conversation.Runtime.
	convCfg := &conversation.OTELConfig{
		Tracer:   otelProvider.Tracer("ycode.conversation"),
		Inst:     otelProvider.Instruments,
		Provider: string(provider.Kind()),
	}

	// Set up request logger for conversation audit.
	if obs.LogConversations {
		reqLogger, err := yotel.NewRequestLogger(instanceDir, yotel.RequestLoggerConfig{
			RetentionDays:  obs.LogRetentionDays,
			LogToolDetails: obs.LogToolDetails,
			Opener:         opener,
		})
		if err != nil {
			slog.Warn("otel: request logger init failed", "error", err)
		} else {
			convCfg.ReqLogger = reqLogger
		}
	}

	// Wire conversation logger for structured OTEL log records to VictoriaLogs.
	if otelProvider.LoggerProvider != nil {
		convCfg.ConvLogger = yotel.NewConversationLogger(otelProvider.LoggerProvider, instanceID)
	}

	// Bridge slog to OTEL LoggerProvider so application logs flow to VictoriaLogs.
	// Use an explicit stderr handler (not slog.Default().Handler()) because
	// slog.SetDefault with a custom handler sets log.SetOutput(io.Discard),
	// which breaks the default handler's underlying log.Writer.
	otelHandler := otelslog.NewHandler("ycode")
	stderrHandler := slog.NewTextHandler(os.Stderr, nil)
	teeHandler := &teeLogHandler{primary: stderrHandler, secondary: otelHandler}
	slog.SetDefault(slog.New(teeHandler))

	// Start retention cleanup goroutine.
	retentionDays := obs.LogRetentionDays
	if retentionDays <= 0 {
		retentionDays = 3
	}
	yotel.StartRetentionCleanup(ctx, dataDir, time.Duration(retentionDays)*24*time.Hour)

	slog.Info("otel: initialized", "collector", collectorAddr, "dataDir", dataDir)

	return &otelResult{
		shutdown: func() {
			// Short timeout: file exports are fast; if gRPC can't flush in 2s
			// (e.g. collector unreachable), waiting longer won't help.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := otelProvider.Shutdown(shutdownCtx); err != nil {
				slog.Warn("otel: shutdown error", "error", err)
			}
		},
		convOTEL: convCfg,
	}
}

// teeLogHandler forwards log records to two handlers.
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
