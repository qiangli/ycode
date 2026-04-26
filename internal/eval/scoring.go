package eval

import (
	"math"
)

// PassAtK computes the pass@k metric: the probability that at least 1 of k
// samples passes, given n total samples with c correct.
//
// Formula: pass@k = 1 - C(n-c, k) / C(n, k)
//
// This is the industry-standard metric from OpenAI's Codex/HumanEval paper.
// Range: [0.0, 1.0]. Higher is better.
func PassAtK(n, c, k int) float64 {
	if n <= 0 || k <= 0 || k > n {
		return 0
	}
	if c >= k {
		// If we have at least k correct, check the exact probability.
	}
	if c >= n {
		return 1.0
	}
	if c <= 0 {
		return 0.0
	}

	// Use log-space to avoid overflow with large binomial coefficients.
	// pass@k = 1 - C(n-c, k) / C(n, k)
	// = 1 - prod_{i=0}^{k-1} (n-c-i) / (n-i)
	logRatio := 0.0
	for i := range k {
		if n-i == 0 {
			return 0
		}
		numerator := float64(n - c - i)
		denominator := float64(n - i)
		if numerator <= 0 {
			// Not enough failures to fill k slots — guaranteed to have a pass.
			return 1.0
		}
		logRatio += math.Log(numerator) - math.Log(denominator)
	}

	return 1.0 - math.Exp(logRatio)
}

// PassPowK computes the pass^k metric: the probability that ALL k samples
// pass, given n total samples with c correct.
//
// Formula: pass^k = C(c, k) / C(n, k)
//
// Measures reliability/consistency, not just capability.
// Range: [0.0, 1.0]. Higher is more consistent.
func PassPowK(n, c, k int) float64 {
	if n <= 0 || k <= 0 || k > n {
		return 0
	}
	if c < k {
		return 0.0
	}
	if c >= n {
		return 1.0
	}

	// pass^k = prod_{i=0}^{k-1} (c-i) / (n-i)
	logRatio := 0.0
	for i := range k {
		if n-i == 0 {
			return 0
		}
		logRatio += math.Log(float64(c-i)) - math.Log(float64(n-i))
	}

	return math.Exp(logRatio)
}

// Flakiness computes the binary entropy of a pass rate, measuring
// how unpredictable/inconsistent a scenario is.
//
// Formula: H(p) = -p·log₂(p) - (1-p)·log₂(1-p)
//
// Range: [0.0, 1.0].
//   - 0.0 = deterministic (all pass or all fail)
//   - 1.0 = maximum entropy (50% pass rate)
func Flakiness(passRate float64) float64 {
	if passRate <= 0 || passRate >= 1 {
		return 0
	}
	return -passRate*math.Log2(passRate) - (1-passRate)*math.Log2(1-passRate)
}

// EditPrecision measures how surgically an agent edited a file.
//
// Formula: 1 - (unintended_changes / total_lines)
//
// Range: [0.0, 1.0]. 1.0 means only target lines were changed.
func EditPrecision(totalLines, unintendedChanges int) float64 {
	if totalLines <= 0 {
		return 1.0
	}
	if unintendedChanges <= 0 {
		return 1.0
	}
	if unintendedChanges >= totalLines {
		return 0.0
	}
	return 1.0 - float64(unintendedChanges)/float64(totalLines)
}

// ToolAccuracy computes the Jaccard similarity between expected and actual
// tool-call sets, measuring whether the agent picked the right tools.
//
// Formula: |expected ∩ actual| / |expected ∪ actual|
//
// Range: [0.0, 1.0]. 1.0 means perfect tool selection.
func ToolAccuracy(expected, actual []string) float64 {
	if len(expected) == 0 && len(actual) == 0 {
		return 1.0
	}

	expectedSet := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		expectedSet[e] = struct{}{}
	}

	actualSet := make(map[string]struct{}, len(actual))
	for _, a := range actual {
		actualSet[a] = struct{}{}
	}

	// Intersection.
	intersection := 0
	for e := range expectedSet {
		if _, ok := actualSet[e]; ok {
			intersection++
		}
	}

	// Union.
	union := make(map[string]struct{})
	for e := range expectedSet {
		union[e] = struct{}{}
	}
	for a := range actualSet {
		union[a] = struct{}{}
	}

	if len(union) == 0 {
		return 1.0
	}

	return float64(intersection) / float64(len(union))
}

// TrajectoryLCS computes the normalized Longest Common Subsequence between
// expected and actual tool-call sequences, measuring trajectory similarity.
//
// Formula: LCS(expected, actual) / max(len(expected), len(actual))
//
// Range: [0.0, 1.0]. 1.0 means identical ordering.
func TrajectoryLCS(expected, actual []string) float64 {
	m := len(expected)
	n := len(actual)
	if m == 0 && n == 0 {
		return 1.0
	}
	if m == 0 || n == 0 {
		return 0.0
	}

	// Standard LCS dynamic programming.
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if expected[i-1] == actual[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	return float64(dp[m][n]) / float64(max(m, n))
}

// CompositeScore computes a single weighted score from individual metrics.
//
// Default weights: pass@k=0.35, pass^k=0.25, (1-flakiness)=0.15,
// toolAccuracy=0.15, costEfficiency=0.10
//
// Range: [0.0, 1.0]. Displayed as 0-100 points.
func CompositeScore(passAtK, passPowK, flakiness, toolAccuracy, costEfficiency float64) float64 {
	return 0.35*passAtK +
		0.25*passPowK +
		0.15*(1.0-flakiness) +
		0.15*toolAccuracy +
		0.10*costEfficiency
}

// CompositeScoreWeighted computes a composite score with custom weights.
// Weights are normalized to sum to 1.0.
func CompositeScoreWeighted(metrics map[string]float64, weights map[string]float64) float64 {
	var totalWeight float64
	for _, w := range weights {
		totalWeight += w
	}
	if totalWeight == 0 {
		return 0
	}

	var score float64
	for name, w := range weights {
		if v, ok := metrics[name]; ok {
			score += (w / totalWeight) * v
		}
	}
	return score
}

// WilsonLower computes the lower bound of the Wilson score confidence interval.
// Used for statistically significant regression detection.
//
// Formula: (p + z²/2n - z·√(p(1-p)/n + z²/4n²)) / (1 + z²/n)
//
// Returns the lower bound at the given confidence level (z=1.96 for 95%).
func WilsonLower(successes, total int, z float64) float64 {
	if total <= 0 {
		return 0
	}
	n := float64(total)
	p := float64(successes) / n

	denominator := 1 + z*z/n
	center := p + z*z/(2*n)
	margin := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n))

	return (center - margin) / denominator
}

// Wilson95Lower is WilsonLower at 95% confidence (z=1.96).
func Wilson95Lower(successes, total int) float64 {
	return WilsonLower(successes, total, 1.96)
}

// RegressionSeverity classifies the severity of a metric change.
type RegressionSeverity int

const (
	SeverityNone       RegressionSeverity = iota // < 5% drop
	SeverityWarning                              // 5-15% drop
	SeverityRegression                           // > 15% drop
)

func (s RegressionSeverity) String() string {
	switch s {
	case SeverityNone:
		return "none"
	case SeverityWarning:
		return "warning"
	case SeverityRegression:
		return "regression"
	default:
		return "unknown"
	}
}

// PercentChange computes the percentage change from baseline to current.
func PercentChange(baseline, current float64) float64 {
	if baseline == 0 {
		if current == 0 {
			return 0
		}
		return 100 // infinite improvement from zero
	}
	return ((current - baseline) / baseline) * 100
}

// ClassifyRegression determines the severity of a metric change.
// A positive delta means improvement; negative means regression.
// The thresholds apply to the absolute drop percentage.
func ClassifyRegression(baseline, current float64) RegressionSeverity {
	pct := PercentChange(baseline, current)
	if pct >= -5 {
		return SeverityNone
	}
	if pct >= -15 {
		return SeverityWarning
	}
	return SeverityRegression
}
