package loopdetect

import (
	"testing"
)

func TestDigest(t *testing.T) {
	a := ToolCall{Name: "bash", Args: `{"command":"ls"}`, Status: "success"}
	b := ToolCall{Name: "bash", Args: `{"command":"ls"}`, Status: "success"}
	c := ToolCall{Name: "bash", Args: `{"command":"pwd"}`, Status: "success"}

	if a.Digest() != b.Digest() {
		t.Error("identical calls should have same digest")
	}
	if a.Digest() == c.Digest() {
		t.Error("different args should produce different digests")
	}
}

func TestOutcomeDigest(t *testing.T) {
	a := ToolCall{Name: "bash", Args: `{"command":"ls"}`, Status: "success", Output: "file.txt"}
	b := ToolCall{Name: "bash", Args: `{"command":"ls"}`, Status: "success", Output: "file.txt"}
	c := ToolCall{Name: "bash", Args: `{"command":"ls"}`, Status: "success", Output: "other.txt"}

	if a.OutcomeDigest() != b.OutcomeDigest() {
		t.Error("identical outcomes should have same digest")
	}
	if a.OutcomeDigest() == c.OutcomeDigest() {
		t.Error("different outputs should produce different digests")
	}
}

func TestGenericRepeat(t *testing.T) {
	th := DefaultThresholds()
	th.GenericRepeatWarning = 3
	th.GenericRepeatCritical = 5
	tracker := New(th, nil)

	call := ToolCall{Name: "bash", Args: `{"command":"ls"}`}

	// First 2 calls: no detection.
	for i := range 2 {
		r := tracker.Record(call)
		if r.Severity != SeverityNone {
			t.Errorf("call %d: expected none, got %s", i+1, r.Severity)
		}
	}

	// 3rd call: warning.
	r := tracker.Record(call)
	if r.Severity != SeverityWarning {
		t.Errorf("call 3: expected warning, got %s", r.Severity)
	}
	if r.Detector != DetectorGenericRepeat {
		t.Errorf("expected generic_repeat detector, got %s", r.Detector)
	}

	// 4th call: still warning.
	r = tracker.Record(call)
	if r.Severity != SeverityWarning {
		t.Errorf("call 4: expected warning, got %s", r.Severity)
	}

	// 5th call: critical.
	r = tracker.Record(call)
	if r.Severity != SeverityCritical {
		t.Errorf("call 5: expected critical, got %s", r.Severity)
	}
}

func TestGenericRepeatReset(t *testing.T) {
	th := DefaultThresholds()
	th.GenericRepeatWarning = 3
	tracker := New(th, nil)

	call := ToolCall{Name: "bash", Args: `{"command":"ls"}`}
	different := ToolCall{Name: "read", Args: `{"path":"/tmp/x"}`}

	tracker.Record(call)
	tracker.Record(call)
	// Interrupt with a different call.
	tracker.Record(different)
	// Restart the repeat.
	r := tracker.Record(call)
	if r.Severity != SeverityNone {
		t.Errorf("expected none after interrupt, got %s", r.Severity)
	}
}

func TestUnknownTool(t *testing.T) {
	th := DefaultThresholds()
	th.UnknownToolMax = 3
	tracker := New(th, nil)
	tracker.RegisterTools([]string{"bash", "read", "write"})

	call := ToolCall{Name: "nonexistent_tool", Args: "{}"}

	tracker.Record(call)
	tracker.Record(call)
	r := tracker.Record(call)
	if r.Severity != SeverityCritical {
		t.Errorf("expected critical after 3 unknown tool calls, got %s", r.Severity)
	}
	if r.Detector != DetectorUnknownTool {
		t.Errorf("expected unknown_tool detector, got %s", r.Detector)
	}
}

func TestUnknownToolKnownInterrupts(t *testing.T) {
	th := DefaultThresholds()
	th.UnknownToolMax = 3
	tracker := New(th, nil)
	tracker.RegisterTools([]string{"bash"})

	unknown := ToolCall{Name: "fake", Args: "{}"}
	known := ToolCall{Name: "bash", Args: `{"cmd":"ls"}`}

	tracker.Record(unknown)
	tracker.Record(unknown)
	// Known tool interrupts the streak.
	tracker.Record(known)
	r := tracker.Record(unknown)
	if r.Severity != SeverityNone {
		t.Errorf("expected none after known tool interrupt, got %s", r.Severity)
	}
}

func TestPollNoProgress(t *testing.T) {
	th := DefaultThresholds()
	th.PollNoProgressWarning = 3
	th.PollNoProgressCritical = 5
	tracker := New(th, nil)

	call := ToolCall{Name: "bash", Args: `{"command":"status"}`, Status: "success", Output: "pending"}

	tracker.Record(call)
	tracker.Record(call)
	r := tracker.Record(call)
	if r.Severity != SeverityWarning {
		t.Errorf("expected warning after 3 polls, got %s", r.Severity)
	}
	if r.Detector != DetectorPollNoProgress {
		t.Errorf("expected poll_no_progress detector, got %s", r.Detector)
	}

	tracker.Record(call)
	r = tracker.Record(call)
	if r.Severity != SeverityCritical {
		t.Errorf("expected critical after 5 polls, got %s", r.Severity)
	}
}

func TestPollProgressResets(t *testing.T) {
	th := DefaultThresholds()
	th.PollNoProgressWarning = 3
	tracker := New(th, nil)

	poll1 := ToolCall{Name: "bash", Args: `{"command":"status"}`, Status: "success", Output: "pending"}
	poll2 := ToolCall{Name: "bash", Args: `{"command":"status"}`, Status: "success", Output: "done"}

	tracker.Record(poll1)
	tracker.Record(poll1)
	// Different output resets the streak.
	tracker.Record(poll2)
	r := tracker.Record(poll1)
	if r.Severity != SeverityNone {
		t.Errorf("expected none after progress, got %s", r.Severity)
	}
}

func TestPingPong(t *testing.T) {
	th := DefaultThresholds()
	th.PingPongWarning = 3
	th.PingPongCritical = 5
	tracker := New(th, nil)

	a := ToolCall{Name: "read", Args: `{"path":"x"}`}
	b := ToolCall{Name: "write", Args: `{"path":"x"}`}

	// Build alternating pattern: a, b, a, b, a, b
	var lastResult Result
	for i := range 6 {
		if i%2 == 0 {
			lastResult = tracker.Record(a)
		} else {
			lastResult = tracker.Record(b)
		}
	}

	if lastResult.Severity < SeverityWarning {
		t.Errorf("expected at least warning after 6 alternations, got %s", lastResult.Severity)
	}
}

func TestGlobalCeiling(t *testing.T) {
	th := DefaultThresholds()
	th.GlobalCeiling = 5
	// Disable other detectors to isolate global ceiling.
	th.GenericRepeatWarning = 0
	th.GenericRepeatCritical = 0
	th.PollNoProgressWarning = 0
	th.PollNoProgressCritical = 0
	tracker := New(th, nil)

	call := ToolCall{Name: "bash", Args: `{"command":"ls"}`}
	other := ToolCall{Name: "read", Args: `{"path":"/tmp"}`}

	// Interleave to avoid generic repeat, but hit global ceiling.
	var r Result
	for range 5 {
		r = tracker.Record(call)
		tracker.Record(other)
	}

	if r.Severity != SeverityCritical {
		t.Errorf("expected critical at global ceiling, got %s", r.Severity)
	}
	if r.Detector != DetectorGlobalCeiling {
		t.Errorf("expected global_ceiling detector, got %s", r.Detector)
	}
}

func TestReset(t *testing.T) {
	tracker := New(DefaultThresholds(), nil)
	call := ToolCall{Name: "bash", Args: `{"command":"ls"}`}
	tracker.Record(call)
	tracker.Record(call)

	tracker.Reset()

	stats := tracker.Stats()
	if stats.HistoryLen != 0 {
		t.Errorf("expected 0 history after reset, got %d", stats.HistoryLen)
	}
	if stats.UniqueDigests != 0 {
		t.Errorf("expected 0 digests after reset, got %d", stats.UniqueDigests)
	}
}

func TestGuidanceMessage(t *testing.T) {
	none := Result{Severity: SeverityNone}
	if msg := GuidanceMessage(none); msg != "" {
		t.Errorf("expected empty guidance for none severity, got %q", msg)
	}

	warning := Result{Severity: SeverityWarning, Detector: DetectorGenericRepeat, Message: "test"}
	msg := GuidanceMessage(warning)
	if msg == "" {
		t.Error("expected non-empty guidance for warning")
	}

	critical := Result{Severity: SeverityCritical, Detector: DetectorPingPong, Message: "test"}
	msg = GuidanceMessage(critical)
	if msg == "" {
		t.Error("expected non-empty guidance for critical")
	}
}

func TestStats(t *testing.T) {
	tracker := New(DefaultThresholds(), nil)
	tracker.RegisterTools([]string{"bash", "read"})

	tracker.Record(ToolCall{Name: "bash", Args: `{"cmd":"a"}`})
	tracker.Record(ToolCall{Name: "bash", Args: `{"cmd":"b"}`})

	stats := tracker.Stats()
	if stats.HistoryLen != 2 {
		t.Errorf("expected 2 history, got %d", stats.HistoryLen)
	}
	if stats.UniqueDigests != 2 {
		t.Errorf("expected 2 unique digests, got %d", stats.UniqueDigests)
	}
	if stats.KnownToolCount != 2 {
		t.Errorf("expected 2 known tools, got %d", stats.KnownToolCount)
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityNone, "none"},
		{SeverityWarning, "warning"},
		{SeverityCritical, "critical"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
