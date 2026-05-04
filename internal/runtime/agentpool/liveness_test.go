package agentpool

import (
	"testing"
	"time"
)

func TestLivenessClassifier_HealthyWhenRecent(t *testing.T) {
	lc := NewLivenessClassifier(DefaultThresholds())
	lc.RecordActivity("a1")

	state := lc.Classify("a1")
	if state != LivenessHealthy {
		t.Errorf("state = %v, want healthy", state)
	}
}

func TestLivenessClassifier_StrandedWhenNeverSeen(t *testing.T) {
	lc := NewLivenessClassifier(DefaultThresholds())

	state := lc.Classify("unknown-agent")
	if state != LivenessStranded {
		t.Errorf("state = %v, want stranded", state)
	}
}

func TestLivenessClassifier_ProgressesThroughStates(t *testing.T) {
	thresholds := LivenessThresholds{
		SuspiciousAfter: 10 * time.Second,
		CriticalAfter:   30 * time.Second,
		StrandedAfter:   60 * time.Second,
	}
	lc := NewLivenessClassifier(thresholds)

	base := time.Now()
	lc.RecordActivity("a1")

	tests := []struct {
		offset time.Duration
		want   LivenessState
	}{
		{5 * time.Second, LivenessHealthy},
		{15 * time.Second, LivenessSuspicious},
		{45 * time.Second, LivenessCritical},
		{90 * time.Second, LivenessStranded},
	}

	for _, tt := range tests {
		got := lc.classifyAt("a1", base.Add(tt.offset))
		if got != tt.want {
			t.Errorf("at +%v: state = %v, want %v", tt.offset, got, tt.want)
		}
	}
}

func TestLivenessClassifier_SelfReportedStuckOverrides(t *testing.T) {
	thresholds := LivenessThresholds{
		SuspiciousAfter: 10 * time.Minute,
		CriticalAfter:   30 * time.Minute,
		StrandedAfter:   2 * time.Hour,
	}
	lc := NewLivenessClassifier(thresholds)

	// Agent has recent activity but reports stuck.
	lc.RecordActivity("a1")
	lc.ReportState("a1", SelfReportStuck)

	state := lc.Classify("a1")
	if state != LivenessCritical {
		t.Errorf("self-reported stuck: state = %v, want critical", state)
	}
}

func TestLivenessClassifier_SelfReportedExitingIsHealthy(t *testing.T) {
	lc := NewLivenessClassifier(DefaultThresholds())
	lc.RecordActivity("a1")
	lc.ReportState("a1", SelfReportExiting)

	state := lc.Classify("a1")
	if state != LivenessHealthy {
		t.Errorf("self-reported exiting: state = %v, want healthy", state)
	}
}

func TestLivenessClassifier_StaleReportIgnored(t *testing.T) {
	thresholds := LivenessThresholds{
		SuspiciousAfter: 10 * time.Second,
		CriticalAfter:   30 * time.Second,
		StrandedAfter:   60 * time.Second,
	}
	lc := NewLivenessClassifier(thresholds)

	base := time.Now()
	lc.mu.Lock()
	lc.lastActivity["a1"] = base
	lc.selfReported["a1"] = SelfReportStuck
	lc.lastReportedAt["a1"] = base
	lc.mu.Unlock()

	// After StrandedAfter, self-report is too old — falls back to freshness.
	got := lc.classifyAt("a1", base.Add(90*time.Second))
	if got != LivenessStranded {
		t.Errorf("stale report: state = %v, want stranded (freshness-based)", got)
	}
}

func TestLivenessClassifier_ScanStale(t *testing.T) {
	thresholds := LivenessThresholds{
		SuspiciousAfter: 10 * time.Second,
		CriticalAfter:   30 * time.Second,
		StrandedAfter:   60 * time.Second,
	}
	lc := NewLivenessClassifier(thresholds)

	base := time.Now()

	lc.mu.Lock()
	lc.lastActivity["fresh"] = base
	lc.lastActivity["stale"] = base.Add(-20 * time.Second)
	lc.lastActivity["dead"] = base.Add(-90 * time.Second)
	lc.mu.Unlock()

	stale := lc.ScanStale(base)
	if len(stale) != 2 {
		t.Fatalf("stale count = %d, want 2", len(stale))
	}
	if stale["stale"] != LivenessSuspicious {
		t.Errorf("stale agent = %v, want suspicious", stale["stale"])
	}
	if stale["dead"] != LivenessStranded {
		t.Errorf("dead agent = %v, want stranded", stale["dead"])
	}
	if _, ok := stale["fresh"]; ok {
		t.Error("fresh agent should not appear in stale scan")
	}
}

func TestLivenessClassifier_Remove(t *testing.T) {
	lc := NewLivenessClassifier(DefaultThresholds())
	lc.RecordActivity("a1")
	lc.ReportState("a1", SelfReportWorking)

	lc.Remove("a1")

	state := lc.Classify("a1")
	if state != LivenessStranded {
		t.Errorf("after remove: state = %v, want stranded", state)
	}
}

func TestLivenessState_String(t *testing.T) {
	tests := []struct {
		s    LivenessState
		want string
	}{
		{LivenessHealthy, "healthy"},
		{LivenessSuspicious, "suspicious"},
		{LivenessCritical, "critical"},
		{LivenessStranded, "stranded"},
		{LivenessState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("LivenessState(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
