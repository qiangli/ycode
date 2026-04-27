package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/tools"
)

func TestDiagnoserRecentReports(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	d := NewDiagnoser(b, nil, time.Minute)

	// No reports initially.
	reports := d.RecentReports(10)
	if len(reports) != 0 {
		t.Fatalf("expected 0 reports, got %d", len(reports))
	}

	// Emit some reports directly.
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		d.emit(ctx, DiagnosticReport{
			ID:       string(rune('a' + i)),
			Severity: SeverityInfo,
			Category: DiagToolDegradation,
			Summary:  "test report",
		})
	}

	// Get all.
	reports = d.RecentReports(10)
	if len(reports) != 5 {
		t.Fatalf("expected 5 reports, got %d", len(reports))
	}

	// Get subset (most recent).
	reports = d.RecentReports(2)
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	// Should be the last two.
	if reports[0].ID != string(rune('a'+3)) || reports[1].ID != string(rune('a'+4)) {
		t.Fatalf("expected last 2 reports, got ids %q and %q", reports[0].ID, reports[1].ID)
	}
}

func TestDiagnoserEmitPublishesBusEvent(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	d := NewDiagnoser(b, nil, time.Minute)

	ch, unsub := b.Subscribe(bus.EventDiagReport)
	defer unsub()

	ctx := context.Background()
	d.emit(ctx, DiagnosticReport{
		ID:       "test-1",
		Severity: SeverityWarn,
		Category: DiagErrorRate,
		Summary:  "high error rate",
	})

	select {
	case ev := <-ch:
		if ev.Type != bus.EventDiagReport {
			t.Fatalf("expected EventDiagReport, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestDiagnoserRingBuffer(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	d := NewDiagnoser(b, nil, time.Minute)
	d.maxReports = 3

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		d.emit(ctx, DiagnosticReport{
			ID:       string(rune('a' + i)),
			Severity: SeverityInfo,
			Category: DiagToolDegradation,
		})
	}

	reports := d.RecentReports(10)
	if len(reports) != 3 {
		t.Fatalf("expected 3 reports (ring buffer), got %d", len(reports))
	}
	// Should have the last 3: c, d, e.
	if reports[0].ID != string(rune('c')) {
		t.Fatalf("expected first report id 'c', got %q", reports[0].ID)
	}
}

func TestDiagnoserStartStop(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	d := NewDiagnoser(b, nil, time.Hour) // long interval so no tick fires

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !d.Healthy() {
		t.Fatal("diagnoser should be healthy after Start")
	}
	if d.Name() != "diagnoser" {
		t.Fatalf("expected name 'diagnoser', got %q", d.Name())
	}

	d.Stop()
	if d.Healthy() {
		t.Fatal("diagnoser should not be healthy after Stop")
	}
}

func TestDiagnoserWithQualityMonitor(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	qm := tools.NewQualityMonitor(0.7)
	// Record some failures to trigger degradation.
	for i := 0; i < 5; i++ {
		qm.RecordCall("broken-tool", false, 100)
	}

	d := NewDiagnoser(b, qm, time.Minute)

	ctx := context.Background()
	d.runDiagnostics(ctx)

	reports := d.RecentReports(10)
	if len(reports) == 0 {
		t.Fatal("expected at least one diagnostic report for degraded tool")
	}
	if reports[0].Category != DiagToolDegradation {
		t.Fatalf("expected category tool_degradation, got %s", reports[0].Category)
	}
	if reports[0].Severity != SeverityCritical {
		t.Fatalf("expected severity critical for 0%% success rate, got %s", reports[0].Severity)
	}
}
