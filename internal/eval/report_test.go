package eval

import (
	"testing"
	"time"
)

func TestReportStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewReportStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	report := &Report{
		ID:        "test-1",
		Version:   "abc123",
		Provider:  "test",
		Model:     "test-model",
		Tier:      "smoke",
		Timestamp: time.Date(2026, 4, 26, 3, 0, 0, 0, time.UTC),
		Composite: 0.82,
		Scenarios: []ScenarioResult{
			{
				Scenario: "arithmetic",
				Tier:     "smoke",
				Policy:   "always_passes",
				Metrics: ScenarioMetrics{
					PassAtK:  1.0,
					PassPowK: 1.0,
				},
			},
		},
	}

	if err := store.Save(report); err != nil {
		t.Fatalf("Save: %v", err)
	}

	files, err := store.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	loaded, err := store.Load(files[0])
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 report, got %d", len(loaded))
	}
	if loaded[0].Composite != 0.82 {
		t.Errorf("composite = %f, want 0.82", loaded[0].Composite)
	}
	if loaded[0].Scenarios[0].Scenario != "arithmetic" {
		t.Errorf("scenario = %q, want 'arithmetic'", loaded[0].Scenarios[0].Scenario)
	}
}

func TestReportStoreLatest(t *testing.T) {
	dir := t.TempDir()
	store, err := NewReportStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Save two reports on different days.
	r1 := &Report{
		ID: "r1", Version: "v1", Provider: "test", Model: "test",
		Tier: "smoke", Timestamp: time.Date(2026, 4, 25, 3, 0, 0, 0, time.UTC),
		Composite: 0.75,
	}
	r2 := &Report{
		ID: "r2", Version: "v2", Provider: "test", Model: "test",
		Tier: "smoke", Timestamp: time.Date(2026, 4, 26, 3, 0, 0, 0, time.UTC),
		Composite: 0.82,
	}

	if err := store.Save(r1); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(r2); err != nil {
		t.Fatal(err)
	}

	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("expected 1 report from latest file, got %d", len(latest))
	}
	if latest[0].Composite != 0.82 {
		t.Errorf("latest composite = %f, want 0.82", latest[0].Composite)
	}
}

func TestCompareReports(t *testing.T) {
	baseline := &Report{
		Version:   "v1",
		Composite: 0.80,
		Scenarios: []ScenarioResult{
			{
				Scenario: "arithmetic",
				Metrics:  ScenarioMetrics{PassAtK: 0.90, PassPowK: 0.70},
			},
			{
				Scenario: "tool_selection",
				Metrics:  ScenarioMetrics{PassAtK: 0.85, ToolAccuracy: 0.90},
			},
		},
	}

	current := &Report{
		Version:   "v2",
		Composite: 0.85,
		Scenarios: []ScenarioResult{
			{
				Scenario: "arithmetic",
				Metrics:  ScenarioMetrics{PassAtK: 0.95, PassPowK: 0.80},
			},
			{
				Scenario: "tool_selection",
				Metrics:  ScenarioMetrics{PassAtK: 0.60, ToolAccuracy: 0.70},
			},
		},
	}

	checks := CompareReports(baseline, current)

	// Should have composite + per-scenario metrics.
	if len(checks) == 0 {
		t.Fatal("expected regression checks")
	}

	// Composite should improve.
	if checks[0].Severity != SeverityNone {
		t.Errorf("composite should be no regression, got %v", checks[0].Severity)
	}

	// tool_selection pass_at_k should regress (0.85 -> 0.60 = -29%).
	found := false
	for _, c := range checks {
		if c.Metric == "tool_selection.pass_at_k" {
			found = true
			if c.Severity != SeverityRegression {
				t.Errorf("tool_selection.pass_at_k severity = %v, want regression", c.Severity)
			}
		}
	}
	if !found {
		t.Error("expected tool_selection.pass_at_k check")
	}

	// HasRegression should detect it.
	if !HasRegression(checks) {
		t.Error("HasRegression should return true")
	}
}

func TestFormatReport(t *testing.T) {
	baseline := &Report{
		Version:   "v1",
		Timestamp: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
		Composite: 0.78,
	}
	current := &Report{
		Version:   "v2",
		Timestamp: time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Composite: 0.82,
	}

	output := FormatReport(baseline, current)
	if output == "" {
		t.Fatal("expected non-empty report")
	}

	// Should contain version info.
	if !contains(output, "v2") || !contains(output, "v1") {
		t.Error("report should contain version strings")
	}

	// No regressions in this case.
	if !contains(output, "Regressions: None") {
		t.Error("expected 'Regressions: None'")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != "" && substr != "" &&
		len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
