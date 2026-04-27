package tools

import (
	"testing"
)

func TestQualityMonitorRecordAndReliability(t *testing.T) {
	qm := NewQualityMonitor(0.7)

	// No calls yet.
	r := qm.Reliability("foo")
	if r.TotalCalls != 0 {
		t.Errorf("expected 0 calls, got %d", r.TotalCalls)
	}

	// Record some calls.
	qm.RecordCall("foo", true, 10.0)
	qm.RecordCall("foo", true, 20.0)
	qm.RecordCall("foo", false, 30.0)

	r = qm.Reliability("foo")
	if r.TotalCalls != 3 {
		t.Fatalf("expected 3 calls, got %d", r.TotalCalls)
	}
	if r.SuccessCount != 2 {
		t.Errorf("expected 2 successes, got %d", r.SuccessCount)
	}
	if r.FailureCount != 1 {
		t.Errorf("expected 1 failure, got %d", r.FailureCount)
	}
	expectedRate := 2.0 / 3.0
	if r.SuccessRate < expectedRate-0.01 || r.SuccessRate > expectedRate+0.01 {
		t.Errorf("expected success rate ~%f, got %f", expectedRate, r.SuccessRate)
	}
	expectedAvg := 20.0
	if r.AvgDurationMs < expectedAvg-0.01 || r.AvgDurationMs > expectedAvg+0.01 {
		t.Errorf("expected avg duration ~%f, got %f", expectedAvg, r.AvgDurationMs)
	}
	if r.LastFailure.IsZero() {
		t.Error("expected non-zero LastFailure")
	}
}

func TestQualityMonitorDegradedTools(t *testing.T) {
	qm := NewQualityMonitor(0.7)

	// Tool with good success rate.
	for i := 0; i < 10; i++ {
		qm.RecordCall("good", true, 5.0)
	}

	// Tool with poor success rate.
	qm.RecordCall("bad", true, 5.0)
	qm.RecordCall("bad", false, 5.0)
	qm.RecordCall("bad", false, 5.0)
	qm.RecordCall("bad", false, 5.0)

	// Tool with too few calls (should not appear).
	qm.RecordCall("few", false, 5.0)

	degraded := qm.DegradedTools()
	if len(degraded) != 1 {
		t.Fatalf("expected 1 degraded tool, got %d", len(degraded))
	}
	if degraded[0].Name != "bad" {
		t.Errorf("expected degraded tool 'bad', got %q", degraded[0].Name)
	}
}

func TestQualityMonitorReset(t *testing.T) {
	qm := NewQualityMonitor(0.7)
	qm.RecordCall("foo", true, 10.0)
	qm.Reset()

	r := qm.Reliability("foo")
	if r.TotalCalls != 0 {
		t.Errorf("expected 0 calls after reset, got %d", r.TotalCalls)
	}
	degraded := qm.DegradedTools()
	if len(degraded) != 0 {
		t.Errorf("expected 0 degraded tools after reset, got %d", len(degraded))
	}
}

func TestQualityMonitorDefaultThreshold(t *testing.T) {
	// Zero threshold should default to 0.7.
	qm := NewQualityMonitor(0)
	if qm.threshold != 0.7 {
		t.Errorf("expected default threshold 0.7, got %f", qm.threshold)
	}
}

func TestRegistryQualityMonitorGetter(t *testing.T) {
	reg := NewRegistry()

	// Initially nil.
	if got := reg.QualityMonitor(); got != nil {
		t.Error("expected nil QualityMonitor before setting")
	}

	// Set and retrieve.
	qm := NewQualityMonitor(0.7)
	reg.SetQualityMonitor(qm)

	got := reg.QualityMonitor()
	if got == nil {
		t.Fatal("expected non-nil QualityMonitor after setting")
	}
	if got != qm {
		t.Error("expected same QualityMonitor instance")
	}
}
