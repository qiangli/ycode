package server

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// OTELConfig holds optional observability instrumentation for the server.
type OTELConfig struct {
	Tracer trace.Tracer
	Meter  metric.Meter
}

// otelMetrics holds the registered OTEL metrics instruments.
type otelMetrics struct {
	requestCount    metric.Int64Counter
	requestDuration metric.Float64Histogram
	wsActiveConns   atomic.Int64
	wsActiveGauge   metric.Int64ObservableGauge
	natsMessages    metric.Int64Counter
}

// setupOTEL initializes OTEL middleware for the server.
// If no tracer/meter are provided, uses the global defaults.
func (s *Server) setupOTEL(cfg *OTELConfig) {
	if cfg == nil {
		return
	}

	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("ycode-server")
	}

	meter := cfg.Meter
	if meter == nil {
		meter = otel.Meter("ycode-server")
	}

	m := &otelMetrics{}

	m.requestCount, _ = meter.Int64Counter("ycode.server.requests",
		metric.WithDescription("Total HTTP requests"),
		metric.WithUnit("{request}"),
	)

	m.requestDuration, _ = meter.Float64Histogram("ycode.server.request_duration",
		metric.WithDescription("HTTP request latency"),
		metric.WithUnit("s"),
	)

	m.natsMessages, _ = meter.Int64Counter("ycode.server.nats_messages",
		metric.WithDescription("Total NATS messages"),
		metric.WithUnit("{message}"),
	)

	m.wsActiveGauge, _ = meter.Int64ObservableGauge("ycode.server.ws_active",
		metric.WithDescription("Active WebSocket connections"),
		metric.WithUnit("{connection}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.wsActiveConns.Load())
			return nil
		}),
	)

	s.otelCfg = cfg
	s.otelMetrics = m
	s.tracer = tracer
}

// otelMiddleware wraps an http.Handler with OTEL tracing and metrics.
func (s *Server) otelMiddleware(next http.Handler) http.Handler {
	if s.otelMetrics == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Start trace span.
		ctx := r.Context()
		if s.tracer != nil {
			var span trace.Span
			ctx, span = s.tracer.Start(ctx, r.Method+" "+r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.Path),
				),
			)
			defer span.End()
			r = r.WithContext(ctx)
		}

		// Wrap response writer to capture status code.
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()

		attrs := metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("path", r.URL.Path),
			attribute.String("status", strconv.Itoa(rw.status)),
		)

		s.otelMetrics.requestCount.Add(ctx, 1, attrs)
		s.otelMetrics.requestDuration.Record(ctx, duration, attrs)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// trackWSConnect increments the active WebSocket connection counter.
func (s *Server) trackWSConnect() {
	if s.otelMetrics != nil {
		s.otelMetrics.wsActiveConns.Add(1)
	}
}

// trackWSDisconnect decrements the active WebSocket connection counter.
func (s *Server) trackWSDisconnect() {
	if s.otelMetrics != nil {
		s.otelMetrics.wsActiveConns.Add(-1)
	}
}
