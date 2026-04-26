package eval

import (
	"math"
	"testing"
)

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

func TestPassAtK(t *testing.T) {
	tests := []struct {
		name     string
		n, c, k  int
		expected float64
	}{
		{"all pass", 3, 3, 3, 1.0},
		{"none pass", 3, 0, 3, 0.0},
		{"2 of 3, k=1", 3, 2, 1, 0.6667},
		{"2 of 3, k=3", 3, 2, 3, 1.0},
		{"1 of 3, k=1", 3, 1, 1, 0.3333},
		{"1 of 3, k=3", 3, 1, 3, 1.0},
		{"5 of 10, k=1", 10, 5, 1, 0.5},
		{"zero n", 0, 0, 1, 0.0},
		{"k > n", 3, 3, 5, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PassAtK(tt.n, tt.c, tt.k)
			if !almostEqual(got, tt.expected, 0.01) {
				t.Errorf("PassAtK(%d, %d, %d) = %.4f, want %.4f", tt.n, tt.c, tt.k, got, tt.expected)
			}
		})
	}
}

func TestPassPowK(t *testing.T) {
	tests := []struct {
		name     string
		n, c, k  int
		expected float64
	}{
		{"all pass", 3, 3, 3, 1.0},
		{"none pass", 3, 0, 3, 0.0},
		{"2 of 3, k=3", 3, 2, 3, 0.0}, // can't pick 3 passes from 2
		{"2 of 3, k=2", 3, 2, 2, 0.3333},
		{"2 of 3, k=1", 3, 2, 1, 0.6667},
		{"zero n", 0, 0, 1, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PassPowK(tt.n, tt.c, tt.k)
			if !almostEqual(got, tt.expected, 0.01) {
				t.Errorf("PassPowK(%d, %d, %d) = %.4f, want %.4f", tt.n, tt.c, tt.k, got, tt.expected)
			}
		})
	}
}

func TestFlakiness(t *testing.T) {
	tests := []struct {
		name     string
		passRate float64
		expected float64
	}{
		{"all pass", 1.0, 0.0},
		{"all fail", 0.0, 0.0},
		{"50/50 = max entropy", 0.5, 1.0},
		{"2 of 3", 0.6667, 0.9183},
		{"1 of 3", 0.3333, 0.9183},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Flakiness(tt.passRate)
			if !almostEqual(got, tt.expected, 0.01) {
				t.Errorf("Flakiness(%.4f) = %.4f, want %.4f", tt.passRate, got, tt.expected)
			}
		})
	}
}

func TestEditPrecision(t *testing.T) {
	tests := []struct {
		name              string
		total, unintended int
		expected          float64
	}{
		{"perfect", 100, 0, 1.0},
		{"all changed", 100, 100, 0.0},
		{"1 unintended out of 100", 100, 1, 0.99},
		{"zero lines", 0, 0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EditPrecision(tt.total, tt.unintended)
			if !almostEqual(got, tt.expected, 0.01) {
				t.Errorf("EditPrecision(%d, %d) = %.4f, want %.4f", tt.total, tt.unintended, got, tt.expected)
			}
		})
	}
}

func TestToolAccuracy(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual []string
		want             float64
	}{
		{"perfect match", []string{"read_file", "edit_file"}, []string{"read_file", "edit_file"}, 1.0},
		{"no overlap", []string{"read_file"}, []string{"bash"}, 0.0},
		{"subset", []string{"read_file", "edit_file"}, []string{"read_file"}, 0.5},
		{"superset", []string{"read_file"}, []string{"read_file", "edit_file"}, 0.5},
		{"both empty", nil, nil, 1.0},
		{"expected empty", nil, []string{"read_file"}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolAccuracy(tt.expected, tt.actual)
			if !almostEqual(got, tt.want, 0.01) {
				t.Errorf("ToolAccuracy(%v, %v) = %.4f, want %.4f", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

func TestTrajectoryLCS(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual []string
		want             float64
	}{
		{"identical", []string{"read_file", "edit_file", "bash"}, []string{"read_file", "edit_file", "bash"}, 1.0},
		{"empty both", nil, nil, 1.0},
		{"empty expected", nil, []string{"read_file"}, 0.0},
		{"reversed", []string{"a", "b", "c"}, []string{"c", "b", "a"}, 0.3333},
		{"subsequence", []string{"a", "b", "c"}, []string{"a", "x", "b", "y", "c"}, 0.6},
		{"extra steps", []string{"read_file", "edit_file"}, []string{"read_file", "grep_search", "edit_file"}, 0.6667},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrajectoryLCS(tt.expected, tt.actual)
			if !almostEqual(got, tt.want, 0.01) {
				t.Errorf("TrajectoryLCS(%v, %v) = %.4f, want %.4f", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

func TestCompositeScore(t *testing.T) {
	// Perfect scores everywhere should give 1.0.
	got := CompositeScore(1.0, 1.0, 0.0, 1.0, 1.0)
	if !almostEqual(got, 1.0, 0.001) {
		t.Errorf("CompositeScore(perfect) = %.4f, want 1.0", got)
	}

	// Zero everywhere should give 0.15 (from 1-flakiness when flakiness=0).
	got = CompositeScore(0.0, 0.0, 0.0, 0.0, 0.0)
	if !almostEqual(got, 0.15, 0.001) {
		t.Errorf("CompositeScore(zero) = %.4f, want 0.15", got)
	}
}

func TestWilson95Lower(t *testing.T) {
	tests := []struct {
		name             string
		successes, total int
		minExpected      float64
	}{
		{"all pass small sample", 3, 3, 0.29}, // wide CI with n=3
		{"all pass large sample", 100, 100, 0.96},
		{"half pass", 50, 100, 0.40},
		{"none pass", 0, 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wilson95Lower(tt.successes, tt.total)
			if got < tt.minExpected {
				t.Errorf("Wilson95Lower(%d, %d) = %.4f, want >= %.4f", tt.successes, tt.total, got, tt.minExpected)
			}
			// Lower bound should always be <= observed rate.
			observedRate := float64(tt.successes) / float64(tt.total)
			if got > observedRate+0.001 {
				t.Errorf("Wilson95Lower(%d, %d) = %.4f > observed rate %.4f", tt.successes, tt.total, got, observedRate)
			}
		})
	}
}

func TestPercentChange(t *testing.T) {
	tests := []struct {
		name              string
		baseline, current float64
		expected          float64
	}{
		{"no change", 0.8, 0.8, 0.0},
		{"improvement", 0.8, 0.9, 12.5},
		{"regression", 0.8, 0.6, -25.0},
		{"zero baseline", 0.0, 0.5, 100.0},
		{"both zero", 0.0, 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PercentChange(tt.baseline, tt.current)
			if !almostEqual(got, tt.expected, 0.1) {
				t.Errorf("PercentChange(%.2f, %.2f) = %.2f, want %.2f", tt.baseline, tt.current, got, tt.expected)
			}
		})
	}
}

func TestClassifyRegression(t *testing.T) {
	tests := []struct {
		name              string
		baseline, current float64
		expected          RegressionSeverity
	}{
		{"no change", 0.8, 0.8, SeverityNone},
		{"improvement", 0.8, 0.9, SeverityNone},
		{"small drop", 0.8, 0.77, SeverityNone},      // -3.75%
		{"warning drop", 0.8, 0.72, SeverityWarning}, // -10%
		{"regression", 0.8, 0.6, SeverityRegression}, // -25%
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyRegression(tt.baseline, tt.current)
			if got != tt.expected {
				t.Errorf("ClassifyRegression(%.2f, %.2f) = %v, want %v", tt.baseline, tt.current, got, tt.expected)
			}
		})
	}
}
