package mesh

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/tools"
)

// Diagnoser observes system health and emits diagnostic reports.
type Diagnoser struct {
	b              bus.Bus
	qualityMonitor *tools.QualityMonitor
	interval       time.Duration
	logger         *slog.Logger

	// State
	cancel     context.CancelFunc
	healthy    atomic.Bool
	mu         sync.Mutex
	reports    []DiagnosticReport // ring buffer of recent reports
	maxReports int

	// Tracking
	consecutiveFailures map[string]int // tool name -> consecutive failure count
	compactionCount     int
	lastCompactionReset time.Time
}

// NewDiagnoser creates a diagnoser agent.
func NewDiagnoser(b bus.Bus, qm *tools.QualityMonitor, interval time.Duration) *Diagnoser {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	return &Diagnoser{
		b:                   b,
		qualityMonitor:      qm,
		interval:            interval,
		logger:              slog.Default(),
		maxReports:          100,
		consecutiveFailures: make(map[string]int),
		lastCompactionReset: time.Now(),
	}
}

func (d *Diagnoser) Name() string  { return "diagnoser" }
func (d *Diagnoser) Healthy() bool { return d.healthy.Load() }

func (d *Diagnoser) Start(ctx context.Context) error {
	ctx, d.cancel = context.WithCancel(ctx)
	d.healthy.Store(true)

	// Background event listener.
	go d.listenEvents(ctx)

	// Periodic tick.
	go d.periodicCheck(ctx)

	return nil
}

func (d *Diagnoser) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.healthy.Store(false)
}

func (d *Diagnoser) listenEvents(ctx context.Context) {
	ch, unsub := d.b.Subscribe(bus.EventToolResult, bus.EventTurnError)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			d.handleEvent(ev)
		}
	}
}

func (d *Diagnoser) handleEvent(ev bus.Event) {
	switch ev.Type {
	case bus.EventToolResult:
		// Track consecutive failures per tool.
		// Parse event data for tool name and success status.
		d.logger.Debug("mesh.diagnoser.event", "type", string(ev.Type))
	case bus.EventTurnError:
		d.logger.Debug("mesh.diagnoser.turn_error", "session", ev.SessionID)
	}
}

func (d *Diagnoser) periodicCheck(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runDiagnostics(ctx)
		}
	}
}

func (d *Diagnoser) runDiagnostics(ctx context.Context) {
	tracer := otel.Tracer("ycode.mesh")
	ctx, span := tracer.Start(ctx, "mesh.diagnoser.tick")
	defer span.End()

	// 1. Check tool degradation.
	if d.qualityMonitor != nil {
		degraded := d.qualityMonitor.DegradedTools()
		for _, tool := range degraded {
			severity := SeverityWarn
			if tool.SuccessRate < 0.5 {
				severity = SeverityCritical
			}
			report := DiagnosticReport{
				ID:        uuid.New().String(),
				Severity:  severity,
				Category:  DiagToolDegradation,
				Summary:   fmt.Sprintf("Tool %q degraded: %.0f%% success rate (%d/%d calls)", tool.Name, tool.SuccessRate*100, tool.SuccessCount, tool.TotalCalls),
				ToolName:  tool.Name,
				Timestamp: time.Now(),
				Evidence: []Evidence{
					{Source: "metrics", Data: fmt.Sprintf("success_rate=%.2f failures=%d", tool.SuccessRate, tool.FailureCount)},
				},
			}
			d.emit(ctx, report)
		}
		span.SetAttributes(attribute.Int("diagnoser.degraded_tools", len(degraded)))
	}

	// 2. Check context overflow frequency.
	d.mu.Lock()
	if time.Since(d.lastCompactionReset) > 10*time.Minute {
		if d.compactionCount > 3 {
			report := DiagnosticReport{
				ID:        uuid.New().String(),
				Severity:  SeverityWarn,
				Category:  DiagContextOverflow,
				Summary:   fmt.Sprintf("Context overflow: %d compactions in 10 minutes", d.compactionCount),
				Timestamp: time.Now(),
			}
			d.emit(ctx, report)
		}
		d.compactionCount = 0
		d.lastCompactionReset = time.Now()
	}
	d.mu.Unlock()

	span.SetAttributes(attribute.Int("diagnoser.reports_total", len(d.reports)))
}

func (d *Diagnoser) emit(ctx context.Context, report DiagnosticReport) {
	d.mu.Lock()
	if len(d.reports) >= d.maxReports {
		d.reports = d.reports[1:] // drop oldest
	}
	d.reports = append(d.reports, report)
	d.mu.Unlock()

	d.logger.Info("mesh.diagnoser.report",
		"id", report.ID,
		"severity", string(report.Severity),
		"category", string(report.Category),
		"summary", report.Summary,
	)

	// Publish on bus.
	d.b.Publish(bus.Event{
		Type: bus.EventDiagReport,
		Data: []byte(fmt.Sprintf(`{"id":%q,"severity":%q,"category":%q,"summary":%q,"tool":%q}`,
			report.ID, report.Severity, report.Category, report.Summary, report.ToolName)),
	})

	span := trace.SpanFromContext(ctx)
	span.AddEvent("diagnostic.report", trace.WithAttributes(
		attribute.String("report.id", report.ID),
		attribute.String("report.severity", string(report.Severity)),
		attribute.String("report.category", string(report.Category)),
	))
}

// RecentReports returns the N most recent diagnostic reports.
func (d *Diagnoser) RecentReports(n int) []DiagnosticReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n >= len(d.reports) {
		result := make([]DiagnosticReport, len(d.reports))
		copy(result, d.reports)
		return result
	}
	result := make([]DiagnosticReport, n)
	copy(result, d.reports[len(d.reports)-n:])
	return result
}
