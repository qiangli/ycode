// Package routing provides multi-factor inference routing that selects
// the optimal model/provider for each task type based on cost, latency,
// quality, local resource pressure, and availability — all informed by
// OTEL telemetry data when available.
package routing

import (
	"math"
	"runtime"
	"time"

	"github.com/qiangli/ycode/internal/runtime/usage"
)

// Factor weights for the routing decision function.
// Each weight determines how much influence a factor has on the final score.
type Weights struct {
	Cost         float64 `json:"cost"`
	Latency      float64 `json:"latency"`
	Quality      float64 `json:"quality"`
	Resource     float64 `json:"resource"`
	Availability float64 `json:"availability"`
}

// DefaultWeights returns the default factor weights.
func DefaultWeights() Weights {
	return Weights{
		Cost:         0.25,
		Latency:      0.30,
		Quality:      0.20,
		Resource:     0.10,
		Availability: 0.15,
	}
}

// TaskBudget defines per-task cost and latency targets.
type TaskBudget struct {
	MaxCostUSD      float64       // max acceptable cost per request
	TargetLatency   time.Duration // target latency for this task type
	EstInputTokens  int           // estimated input tokens for cost calculation
	EstOutputTokens int           // estimated output tokens for cost calculation
}

// DefaultTaskBudgets returns budgets for each task type.
func DefaultTaskBudgets() map[TaskType]TaskBudget {
	return map[TaskType]TaskBudget{
		TaskClassification: {
			MaxCostUSD: 0.001, TargetLatency: 500 * time.Millisecond,
			EstInputTokens: 150, EstOutputTokens: 30,
		},
		TaskEmbedding: {
			MaxCostUSD: 0.0005, TargetLatency: 200 * time.Millisecond,
			EstInputTokens: 500, EstOutputTokens: 0,
		},
		TaskSummarization: {
			MaxCostUSD: 0.01, TargetLatency: 5 * time.Second,
			EstInputTokens: 4000, EstOutputTokens: 500,
		},
		TaskCommitMsg: {
			MaxCostUSD: 0.005, TargetLatency: 3 * time.Second,
			EstInputTokens: 2000, EstOutputTokens: 100,
		},
	}
}

// CandidateStats holds observed performance data for a model candidate.
// All fields are optional — zero values mean "no data available".
type CandidateStats struct {
	ObservedP50Ms float64 // observed median latency in milliseconds
	SuccessRate   float64 // 0.0-1.0, from recent calls
	SampleCount   int     // number of observations (0 = cold start)
}

// ScoreCost returns a 0.0-1.0 score based on estimated dollar cost.
// Local models always score 1.0. Remote models score higher when cheap.
func ScoreCost(model string, isLocal bool, budget TaskBudget) float64 {
	if isLocal {
		return 1.0
	}
	cost := usage.EstimateCost(model, budget.EstInputTokens, budget.EstOutputTokens, 0, 0)
	if budget.MaxCostUSD <= 0 {
		return 0.5 // no budget configured
	}
	ratio := cost / budget.MaxCostUSD
	return clamp01(1.0 - ratio)
}

// ScoreLatency returns a 0.0-1.0 score based on observed or estimated latency.
// Lower latency scores higher.
func ScoreLatency(stats CandidateStats, isLocal bool, budget TaskBudget) float64 {
	targetMs := float64(budget.TargetLatency.Milliseconds())
	if targetMs <= 0 {
		return 0.5
	}

	observedMs := stats.ObservedP50Ms
	if stats.SampleCount == 0 {
		// Cold start defaults.
		if isLocal {
			observedMs = 500 // local models typically 300-800ms
		} else {
			observedMs = 400 // remote cheap models typically 200-600ms
		}
	}

	ratio := observedMs / targetMs
	return clamp01(1.0 - ratio)
}

// ScoreQuality returns a 0.0-1.0 score based on model capability vs task complexity.
// This is a static mapping — larger/more capable models score higher for complex tasks.
func ScoreQuality(model string, isLocal bool, task TaskType) float64 {
	switch task {
	case TaskClassification:
		// Any model can classify. Local models are fine.
		if isLocal {
			return 0.85
		}
		return 0.95

	case TaskEmbedding:
		// Specialized embedding models are best. General models are adequate.
		if isLocal {
			return 0.90 // local embedding models are often specialized
		}
		return 0.85

	case TaskSummarization:
		// Larger models produce better summaries.
		if isLocal {
			return 0.50 // 7B models are decent but miss nuance
		}
		// Check model class for remote.
		return remoteQualityScore(model)

	case TaskCommitMsg:
		if isLocal {
			return 0.60
		}
		return remoteQualityScore(model)

	default:
		return 0.5
	}
}

// remoteQualityScore returns a quality score based on known model capabilities.
func remoteQualityScore(model string) float64 {
	// Prefix matching for known model families.
	prefixes := map[string]float64{
		"claude-opus":      1.0,
		"claude-sonnet":    0.85,
		"claude-haiku":     0.70,
		"gpt-4o-mini":      0.65,
		"gpt-4o":           0.85,
		"o3":               0.95,
		"o4-mini":          0.75,
		"gemini-2.5-pro":   0.85,
		"gemini-2.5-flash": 0.70,
	}

	// Longest prefix match.
	bestLen := 0
	bestScore := 0.70 // default
	for prefix, score := range prefixes {
		if len(model) >= len(prefix) && model[:len(prefix)] == prefix && len(prefix) > bestLen {
			bestLen = len(prefix)
			bestScore = score
		}
	}
	return bestScore
}

// ScoreResource returns a 0.0-1.0 score based on local resource pressure.
// Remote models always score 1.0 (no local resource impact).
// Local models score lower when the system is under load.
func ScoreResource(isLocal bool, loadAvg float64) float64 {
	if !isLocal {
		return 1.0
	}
	numCPU := float64(runtime.NumCPU())
	if numCPU <= 0 {
		numCPU = 1
	}
	utilization := loadAvg / numCPU
	return clamp01(1.0 - utilization)
}

// ScoreAvailability returns a 0.0-1.0 score based on observed success rate.
// No data yet gets an optimistic default.
func ScoreAvailability(stats CandidateStats) float64 {
	if stats.SampleCount == 0 {
		return 0.80 // optimistic default for cold start
	}
	return clamp01(stats.SuccessRate)
}

// CompositeScore computes the weighted sum of all factor scores.
func CompositeScore(w Weights, cost, latency, quality, resource, availability float64) float64 {
	return w.Cost*cost +
		w.Latency*latency +
		w.Quality*quality +
		w.Resource*resource +
		w.Availability*availability
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
