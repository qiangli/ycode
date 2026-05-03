package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/bus"
)

// FixResult records the outcome of a fix attempt.
type FixResult struct {
	ReportID string `json:"report_id"`
	Success  bool   `json:"success"`
	Action   string `json:"action"` // "code_fix", "config_fix", "skip", "escalate"
	Detail   string `json:"detail"`
}

// Fixer reacts to diagnostic reports and attempts remediation.
type Fixer struct {
	b      bus.Bus
	safety *SafetyGuard
	logger *slog.Logger

	// FixFunc is called to attempt a fix. Injected by caller to avoid
	// tight coupling to selfheal internals. Receives the diagnostic report
	// and returns whether the fix succeeded.
	FixFunc func(ctx context.Context, report DiagnosticReport) (FixResult, error)

	cancel  context.CancelFunc
	healthy atomic.Bool
}

// NewFixer creates a fixer agent.
func NewFixer(b bus.Bus, safety *SafetyGuard) *Fixer {
	return &Fixer{
		b:      b,
		safety: safety,
		logger: slog.Default(),
	}
}

func (f *Fixer) Name() string  { return "fixer" }
func (f *Fixer) Healthy() bool { return f.healthy.Load() }

func (f *Fixer) Start(ctx context.Context) error {
	ctx, f.cancel = context.WithCancel(ctx)
	f.healthy.Store(true)
	// Subscribe before launching the goroutine so no events are missed.
	ch, unsub := f.b.Subscribe(bus.EventDiagReport)
	go f.listen(ctx, ch, unsub)
	return nil
}

func (f *Fixer) Stop() {
	if f.cancel != nil {
		f.cancel()
	}
	f.healthy.Store(false)
}

func (f *Fixer) listen(ctx context.Context, ch <-chan bus.Event, unsub func()) {
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			f.handleReport(ctx, ev)
		}
	}
}

func (f *Fixer) handleReport(ctx context.Context, ev bus.Event) {
	// Parse the diagnostic report from event data.
	var reportSummary struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Category string `json:"category"`
		Summary  string `json:"summary"`
		Tool     string `json:"tool"`
	}
	if err := json.Unmarshal(ev.Data, &reportSummary); err != nil {
		f.logger.Debug("mesh.fixer.parse_error", "error", err)
		return
	}

	// Only act on warn/critical severity.
	if reportSummary.Severity != string(SeverityWarn) && reportSummary.Severity != string(SeverityCritical) {
		return
	}

	// Check safety guard.
	canFix, reason := f.safety.CanFix(reportSummary.ID)
	if !canFix {
		f.logger.Info("mesh.fixer.blocked", "report_id", reportSummary.ID, "reason", reason)
		f.b.Publish(bus.Event{Type: bus.EventFixFailed, Data: []byte(fmt.Sprintf(`{"report_id":%q,"reason":%q}`, reportSummary.ID, reason))})
		return
	}

	tracer := otel.Tracer("ycode.mesh")
	_, span := tracer.Start(ctx, "mesh.fixer.attempt",
		trace.WithAttributes(
			attribute.String("report.id", reportSummary.ID),
			attribute.String("report.category", reportSummary.Category),
			attribute.String("report.severity", reportSummary.Severity),
		),
	)
	defer span.End()

	f.b.Publish(bus.Event{Type: bus.EventFixStart, Data: ev.Data})
	f.safety.RecordFix(reportSummary.ID)

	f.logger.Info("mesh.fixer.attempting",
		"report_id", reportSummary.ID,
		"category", reportSummary.Category,
		"tool", reportSummary.Tool,
	)

	if f.FixFunc == nil {
		f.logger.Warn("mesh.fixer.no_fix_func", "report_id", reportSummary.ID)
		f.b.Publish(bus.Event{Type: bus.EventFixFailed, Data: []byte(fmt.Sprintf(`{"report_id":%q,"reason":"no FixFunc configured"}`, reportSummary.ID))})
		return
	}

	report := DiagnosticReport{
		ID:       reportSummary.ID,
		Severity: Severity(reportSummary.Severity),
		Category: DiagCategory(reportSummary.Category),
		Summary:  reportSummary.Summary,
		ToolName: reportSummary.Tool,
	}

	result, err := f.FixFunc(ctx, report)
	if err != nil {
		f.logger.Error("mesh.fixer.error", "report_id", reportSummary.ID, "error", err)
		f.b.Publish(bus.Event{Type: bus.EventFixFailed, Data: []byte(fmt.Sprintf(`{"report_id":%q,"error":%q}`, reportSummary.ID, err.Error()))})
		return
	}

	if result.Success {
		f.logger.Info("mesh.fixer.success", "report_id", reportSummary.ID, "action", result.Action)
		f.b.Publish(bus.Event{Type: bus.EventFixComplete, Data: mustMarshal(result)})
	} else {
		f.logger.Warn("mesh.fixer.failed", "report_id", reportSummary.ID, "action", result.Action, "detail", result.Detail)
		f.b.Publish(bus.Event{Type: bus.EventFixFailed, Data: mustMarshal(result)})
	}
}

func mustMarshal(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
