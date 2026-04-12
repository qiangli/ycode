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

// setupOTEL initializes OTEL instrumentation and returns the result.
// It is non-blocking — if initialization fails, it logs a warning and returns a no-op.
func setupOTEL(cfg *config.Config, sess *session.Session, toolReg *tools.Registry, provider api.Provider) *otelResult {
	obs := cfg.Observability
	home, _ := os.UserHomeDir()

	dataDir := obs.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(home, ".ycode", "otel")
	}

	collectorAddr := obs.CollectorAddr
	if collectorAddr == "" && !noOTEL {
		// Use the embedded collector's gRPC port (4317 by default).
		collectorAddr = "127.0.0.1:4317"
	}

	sampleRate := obs.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	// Create OTEL provider.
	ctx := context.Background()
	// Generate a unique instance ID for this ycode process.
	instanceID := sess.ID // Use session ID as instance identifier — unique per process.

	otelProvider, err := yotel.NewProvider(ctx, yotel.ProviderConfig{
		CollectorAddr:  collectorAddr,
		ServiceName:    "ycode",
		ServiceVersion: version,
		SessionID:      sess.ID,
		InstanceID:     instanceID,
		SampleRate:     sampleRate,
		DataDir:        dataDir,
		PersistTraces:  obs.PersistTraces,
		PersistMetrics: obs.PersistMetrics,
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
		reqLogger, err := yotel.NewRequestLogger(dataDir, yotel.RequestLoggerConfig{
			RetentionDays:  obs.LogRetentionDays,
			LogToolDetails: obs.LogToolDetails,
		})
		if err != nil {
			slog.Warn("otel: request logger init failed", "error", err)
		} else {
			convCfg.ReqLogger = reqLogger
		}
	}

	// Bridge slog to OTEL LoggerProvider.
	otelHandler := otelslog.NewHandler("ycode")
	originalHandler := slog.Default().Handler()
	teeHandler := &teeLogHandler{primary: originalHandler, secondary: otelHandler}
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
