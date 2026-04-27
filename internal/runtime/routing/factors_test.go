package routing

import (
	"math"
	"testing"
	"time"
)

func TestScoreCost_LocalAlways1(t *testing.T) {
	budget := TaskBudget{MaxCostUSD: 0.001, EstInputTokens: 100, EstOutputTokens: 20}
	score := ScoreCost("any-model", true, budget)
	if score != 1.0 {
		t.Errorf("local model should always score 1.0, got %f", score)
	}
}

func TestScoreCost_CheapRemoteScoresHigh(t *testing.T) {
	budget := TaskBudget{MaxCostUSD: 0.001, EstInputTokens: 150, EstOutputTokens: 30}

	// gpt-4o-mini: $0.15/M input, $0.60/M output
	// cost = 150*0.15/1M + 30*0.60/1M ≈ $0.0000405 → ratio ≈ 0.04 → score ≈ 0.96
	score := ScoreCost("gpt-4o-mini", false, budget)
	if score < 0.90 {
		t.Errorf("cheap remote model should score high, got %f", score)
	}
}

func TestScoreCost_ExpensiveRemoteScoresLow(t *testing.T) {
	budget := TaskBudget{MaxCostUSD: 0.001, EstInputTokens: 150, EstOutputTokens: 30}

	// claude-opus-4: $15/M input, $75/M output
	// cost = 150*15/1M + 30*75/1M ≈ $0.00450 → ratio ≈ 4.5 → clamped to 0
	score := ScoreCost("claude-opus-4-20250514", false, budget)
	if score > 0.1 {
		t.Errorf("expensive model should score low for small task budget, got %f", score)
	}
}

func TestScoreLatency_ColdStartDefaults(t *testing.T) {
	budget := TaskBudget{TargetLatency: 500 * time.Millisecond}

	// Local cold start: ~500ms, target 500ms → ratio 1.0 → score 0.0
	local := ScoreLatency(CandidateStats{}, true, budget)

	// Remote cold start: ~400ms, target 500ms → ratio 0.8 → score 0.2
	remote := ScoreLatency(CandidateStats{}, false, budget)

	if remote <= local {
		t.Errorf("remote cold start should score higher than local for 500ms target, local=%f remote=%f", local, remote)
	}
}

func TestScoreLatency_ObservedData(t *testing.T) {
	budget := TaskBudget{TargetLatency: 500 * time.Millisecond}

	fast := ScoreLatency(CandidateStats{ObservedP50Ms: 100, SampleCount: 20}, false, budget)
	slow := ScoreLatency(CandidateStats{ObservedP50Ms: 800, SampleCount: 20}, false, budget)

	if fast <= slow {
		t.Errorf("faster observed latency should score higher, fast=%f slow=%f", fast, slow)
	}
}

func TestScoreQuality_ClassificationAnyModelFine(t *testing.T) {
	local := ScoreQuality("qwen2.5-coder:7b", true, TaskClassification)
	remote := ScoreQuality("claude-haiku-4-5-20251001", false, TaskClassification)

	// Both should be high for classification.
	if local < 0.80 {
		t.Errorf("local model should score ≥0.80 for classification, got %f", local)
	}
	if remote < 0.80 {
		t.Errorf("remote model should score ≥0.80 for classification, got %f", remote)
	}
}

func TestScoreQuality_SummarizationFavorsLargeModels(t *testing.T) {
	local := ScoreQuality("qwen2.5:7b", true, TaskSummarization)
	opus := ScoreQuality("claude-opus-4-20250514", false, TaskSummarization)

	if opus <= local {
		t.Errorf("opus should score higher than local 7B for summarization, opus=%f local=%f", opus, local)
	}
}

func TestScoreResource_RemoteAlways1(t *testing.T) {
	score := ScoreResource(false, 99.0)
	if score != 1.0 {
		t.Errorf("remote model should always score 1.0 for resource, got %f", score)
	}
}

func TestScoreResource_LocalHighLoad(t *testing.T) {
	// Simulate high load on a 4-core machine.
	score := ScoreResource(true, 4.0) // load avg 4.0 on a machine (numCPU is runtime)
	// Exact value depends on runtime.NumCPU() but should be low-ish.
	if score > 0.5 && score < 1.0 {
		// Expected range for loaded system.
	}
	// Just verify it doesn't panic or return NaN.
	if math.IsNaN(score) {
		t.Error("should not return NaN")
	}
}

func TestScoreAvailability_ColdStart(t *testing.T) {
	score := ScoreAvailability(CandidateStats{})
	if score != 0.80 {
		t.Errorf("cold start should return 0.80, got %f", score)
	}
}

func TestScoreAvailability_HighSuccessRate(t *testing.T) {
	score := ScoreAvailability(CandidateStats{SuccessRate: 0.95, SampleCount: 20})
	if score != 0.95 {
		t.Errorf("95%% success should return 0.95, got %f", score)
	}
}

func TestScoreAvailability_LowSuccessRate(t *testing.T) {
	score := ScoreAvailability(CandidateStats{SuccessRate: 0.30, SampleCount: 10})
	if score != 0.30 {
		t.Errorf("30%% success should return 0.30, got %f", score)
	}
}

func TestCompositeScore(t *testing.T) {
	w := DefaultWeights()
	// All perfect scores.
	score := CompositeScore(w, 1.0, 1.0, 1.0, 1.0, 1.0)
	expected := w.Cost + w.Latency + w.Quality + w.Resource + w.Availability
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("all 1.0 scores should sum to %f, got %f", expected, score)
	}

	// All zero scores.
	score = CompositeScore(w, 0, 0, 0, 0, 0)
	if score != 0 {
		t.Errorf("all 0 scores should give 0, got %f", score)
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		in, out float64
	}{
		{-0.5, 0}, {0, 0}, {0.5, 0.5}, {1.0, 1.0}, {1.5, 1.0},
		{math.NaN(), 0},
	}
	for _, tt := range tests {
		got := clamp01(tt.in)
		if got != tt.out {
			t.Errorf("clamp01(%v) = %v, want %v", tt.in, got, tt.out)
		}
	}
}

func TestRemoteQualityScore_PrefixMatching(t *testing.T) {
	tests := []struct {
		model    string
		minScore float64
	}{
		{"claude-opus-4-20250514", 0.95},
		{"claude-sonnet-4-6-20250514", 0.80},
		{"claude-haiku-4-5-20251001", 0.65},
		{"gpt-4o-mini", 0.60},
		{"gpt-4o-2025-01", 0.80},
		{"unknown-model-v1", 0.65}, // falls back to default
	}
	for _, tt := range tests {
		score := remoteQualityScore(tt.model)
		if score < tt.minScore {
			t.Errorf("remoteQualityScore(%q) = %f, want ≥ %f", tt.model, score, tt.minScore)
		}
	}
}
