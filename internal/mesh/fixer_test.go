package mesh

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestFixer_SafetyBlocking(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	// Allow only 1 fix attempt per report.
	safety := NewSafetyGuard(10, 1)

	fixer := NewFixer(mb, safety)
	fixer.FixFunc = func(_ context.Context, report DiagnosticReport) (FixResult, error) {
		return FixResult{ReportID: report.ID, Success: true, Action: "code_fix"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fixer.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer fixer.Stop()

	// Subscribe to fix events.
	fixCh, unsub := mb.Subscribe(bus.EventFixComplete, bus.EventFixFailed)
	defer unsub()

	report := map[string]string{
		"id":       "report-1",
		"severity": "warn",
		"category": "tool_degradation",
		"summary":  "test degradation",
		"tool":     "bash",
	}
	data, _ := json.Marshal(report)

	// First attempt should succeed.
	mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: data})

	select {
	case ev := <-fixCh:
		if ev.Type != bus.EventFixComplete {
			t.Fatalf("expected fix.complete, got %s", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first fix")
	}

	// Second attempt should be blocked by safety guard.
	mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: data})

	select {
	case ev := <-fixCh:
		if ev.Type != bus.EventFixFailed {
			t.Fatalf("expected fix.failed, got %s", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for blocked fix")
	}
}

func TestFixer_IgnoresInfoSeverity(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	safety := NewSafetyGuard(10, 5)

	called := false
	fixer := NewFixer(mb, safety)
	fixer.FixFunc = func(_ context.Context, _ DiagnosticReport) (FixResult, error) {
		called = true
		return FixResult{Success: true}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fixer.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer fixer.Stop()

	report := map[string]string{
		"id":       "report-info",
		"severity": "info",
		"category": "tool_degradation",
		"summary":  "info level report",
	}
	data, _ := json.Marshal(report)
	mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: data})

	// Give the listener time to process.
	time.Sleep(100 * time.Millisecond)

	if called {
		t.Fatal("fixer should not act on info severity")
	}
}

func TestFixer_HourlyBudgetExhaustion(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	// Allow only 2 fixes per hour, but many per report.
	safety := NewSafetyGuard(2, 10)

	fixCount := 0
	fixer := NewFixer(mb, safety)
	fixer.FixFunc = func(_ context.Context, report DiagnosticReport) (FixResult, error) {
		fixCount++
		return FixResult{ReportID: report.ID, Success: true, Action: "code_fix"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fixer.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer fixer.Stop()

	resultCh, unsub := mb.Subscribe(bus.EventFixComplete, bus.EventFixFailed)
	defer unsub()

	// Send 3 different reports.
	for i := 0; i < 3; i++ {
		report := map[string]string{
			"id":       "report-budget-" + string(rune('a'+i)),
			"severity": "warn",
			"category": "error_rate",
			"summary":  "test",
		}
		data, _ := json.Marshal(report)
		mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: data})

		select {
		case <-resultCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout on report %d", i)
		}
	}

	// Only 2 should have been fixed.
	if fixCount != 2 {
		t.Fatalf("expected 2 fixes, got %d", fixCount)
	}
}
