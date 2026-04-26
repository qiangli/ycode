package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Report captures a complete eval run with all scenario results.
type Report struct {
	ID        string           `json:"id"`
	Version   string           `json:"version"`
	Provider  string           `json:"provider"`
	Model     string           `json:"model"`
	Tier      string           `json:"tier"`
	Timestamp time.Time        `json:"timestamp"`
	Scenarios []ScenarioResult `json:"scenarios"`
	Composite float64          `json:"composite_score"`
	Duration  time.Duration    `json:"duration_ms"`
}

// ReportStore persists and retrieves eval reports as JSONL files.
// Each run produces a single JSONL line in {date}-{version}.jsonl.
type ReportStore struct {
	dir string
}

// NewReportStore creates a store at the given directory.
func NewReportStore(dir string) (*ReportStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create report dir: %w", err)
	}
	return &ReportStore{dir: dir}, nil
}

// Save appends a report to the JSONL file for the current day.
func (s *ReportStore) Save(r *Report) error {
	filename := fmt.Sprintf("%s-%s.jsonl", r.Timestamp.Format("2006-01-02"), sanitize(r.Version))
	path := filepath.Join(s.dir, filename)

	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open report file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// Load reads all reports from a JSONL file.
func (s *ReportStore) Load(filename string) ([]Report, error) {
	path := filepath.Join(s.dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report file: %w", err)
	}

	var reports []Report
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r Report
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("unmarshal report line: %w", err)
		}
		reports = append(reports, r)
	}
	return reports, nil
}

// ListFiles returns all report files sorted by name (most recent last).
func (s *ReportStore) ListFiles() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list report dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// Latest loads the most recent report file and returns its reports.
func (s *ReportStore) Latest() ([]Report, error) {
	files, err := s.ListFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no report files found")
	}
	return s.Load(files[len(files)-1])
}

// RegressionCheck compares a current report against a baseline and returns
// a human-readable regression analysis.
type RegressionCheck struct {
	Metric   string             `json:"metric"`
	Baseline float64            `json:"baseline"`
	Current  float64            `json:"current"`
	Delta    float64            `json:"delta_pct"`
	Severity RegressionSeverity `json:"severity"`
}

// CompareReports generates a regression analysis between two reports.
func CompareReports(baseline, current *Report) []RegressionCheck {
	var checks []RegressionCheck

	addCheck := func(name string, b, c float64) {
		checks = append(checks, RegressionCheck{
			Metric:   name,
			Baseline: b,
			Current:  c,
			Delta:    PercentChange(b, c),
			Severity: ClassifyRegression(b, c),
		})
	}

	addCheck("composite_score", baseline.Composite, current.Composite)

	// Build maps of scenario metrics for comparison.
	baseScenarios := make(map[string]ScenarioMetrics)
	for _, s := range baseline.Scenarios {
		baseScenarios[s.Scenario] = s.Metrics
	}

	for _, s := range current.Scenarios {
		if bm, ok := baseScenarios[s.Scenario]; ok {
			prefix := s.Scenario + "."
			addCheck(prefix+"pass_at_k", bm.PassAtK, s.Metrics.PassAtK)
			addCheck(prefix+"pass_pow_k", bm.PassPowK, s.Metrics.PassPowK)
			if bm.ToolAccuracy > 0 || s.Metrics.ToolAccuracy > 0 {
				addCheck(prefix+"tool_accuracy", bm.ToolAccuracy, s.Metrics.ToolAccuracy)
			}
			if bm.TrajectoryScore > 0 || s.Metrics.TrajectoryScore > 0 {
				addCheck(prefix+"trajectory_score", bm.TrajectoryScore, s.Metrics.TrajectoryScore)
			}
		}
	}

	return checks
}

// FormatReport produces a human-readable regression report string.
func FormatReport(baseline, current *Report) string {
	checks := CompareReports(baseline, current)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Eval Report: %s (%s) vs %s (%s)\n",
		current.Version, current.Timestamp.Format("2006-01-02"),
		baseline.Version, baseline.Timestamp.Format("2006-01-02"))
	sb.WriteString(strings.Repeat("=", 60) + "\n")

	fmt.Fprintf(&sb, "Composite Score:  %.0f/100 -> %.0f/100  (%+.1f%%)\n",
		baseline.Composite*100, current.Composite*100,
		PercentChange(baseline.Composite, current.Composite))
	sb.WriteString("\n")

	var regressions, warnings, improvements []RegressionCheck
	for _, c := range checks {
		switch c.Severity {
		case SeverityRegression:
			regressions = append(regressions, c)
		case SeverityWarning:
			warnings = append(warnings, c)
		case SeverityNone:
			if c.Delta > 5 {
				improvements = append(improvements, c)
			}
		}
	}

	if len(regressions) > 0 {
		sb.WriteString("Regressions (>15% drop):\n")
		for _, r := range regressions {
			fmt.Fprintf(&sb, "  %s: %.4f -> %.4f (%+.1f%%)\n", r.Metric, r.Baseline, r.Current, r.Delta)
		}
	} else {
		sb.WriteString("Regressions: None\n")
	}

	if len(warnings) > 0 {
		sb.WriteString("\nWarnings (5-15% drop):\n")
		for _, w := range warnings {
			fmt.Fprintf(&sb, "  %s: %.4f -> %.4f (%+.1f%%)\n", w.Metric, w.Baseline, w.Current, w.Delta)
		}
	}

	if len(improvements) > 0 {
		sb.WriteString("\nImprovements:\n")
		for _, i := range improvements {
			fmt.Fprintf(&sb, "  %s: %.4f -> %.4f (%+.1f%%)\n", i.Metric, i.Baseline, i.Current, i.Delta)
		}
	}

	return sb.String()
}

// HasRegression returns true if any check shows a regression.
func HasRegression(checks []RegressionCheck) bool {
	for _, c := range checks {
		if c.Severity == SeverityRegression {
			return true
		}
	}
	return false
}

func sanitize(s string) string {
	r := strings.NewReplacer("/", "-", " ", "-", ":", "-")
	return r.Replace(s)
}
