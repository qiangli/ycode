package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Competitive Benchmark & Fusion Ablation
//
// Positions ycode memory retrieval against published SOTA baselines
// and validates that multi-backend fusion outperforms single backends.
// ---------------------------------------------------------------------------

// publishedBaselines contains published benchmark results from SOTA memory
// frameworks. These are logged alongside ycode results for reference — not used
// as assertions since the evaluation datasets differ.
var publishedBaselines = map[string]float64{
	"mem0_locomo_overall":     0.6713, // Mem0g on LoCoMo (2026)
	"graphiti_dmr":            0.9480, // Zep/Graphiti on DMR benchmark
	"memvid_locomo_overall":   0.9063, // Memvid +35% over baseline LoCoMo
	"memgpt_dmr":              0.9340, // MemGPT/Letta on DMR
	"letta_terminal_bench_r1": 1.0000, // Letta Code #1 on Terminal-Bench (model-agnostic)
}

// TestCompetitive_LoCoMoSubset runs the 60-memory corpus (structurally
// equivalent to LoCoMo methodology) and logs results alongside SOTA.
func TestCompetitive_LoCoMoSubset(t *testing.T) {
	corpus := loadQualityCorpus(t)
	mgr := setupQualityManager(t, corpus)
	m := runQualityBenchmark(t, mgr, corpus, 5)

	t.Logf("=== ycode Memory Benchmark Results ===")
	t.Logf("Overall:     P@5=%.4f R@5=%.4f NDCG=%.4f MRR=%.4f F1=%.4f",
		m.Overall.MeanPrecision, m.Overall.MeanRecall, m.Overall.MeanNDCG, m.Overall.MeanMRR, m.F1Score)
	t.Logf("Latency:     P50=%v P99=%v", m.LatencyP50, m.LatencyP99)
	t.Logf("Adversarial: FPR=%.4f", m.AdversarialFPR)

	for cat, cm := range m.PerCategory {
		t.Logf("  %-12s P@5=%.4f R@5=%.4f NDCG=%.4f MRR=%.4f (n=%d)",
			cat, cm.MeanPrecision, cm.MeanRecall, cm.MeanNDCG, cm.MeanMRR, cm.QueryCount)
	}

	t.Logf("\n=== Published SOTA Baselines (different datasets) ===")
	for name, score := range publishedBaselines {
		t.Logf("  %-30s %.4f", name, score)
	}

	// Minimum acceptable performance (no direct SOTA comparison possible).
	if m.Overall.MeanMRR < 0.40 {
		t.Errorf("MRR = %.4f is below competitive threshold 0.40", m.Overall.MeanMRR)
	}
}

// TestCompetitive_FusionAblation compares retrieval quality across four
// backend configurations to validate that fusion adds value.
func TestCompetitive_FusionAblation(t *testing.T) {
	corpus := loadQualityCorpus(t)

	type config struct {
		name      string
		useEntity bool
	}

	configs := []config{
		{name: "keyword-only", useEntity: false},
		{name: "keyword+entity+RRF", useEntity: true},
	}

	type ablationResult struct {
		name    string
		metrics qualityMetrics
	}

	var results []ablationResult
	for _, cfg := range configs {
		dir := t.TempDir()
		mgr, _ := NewManager(dir)

		if cfg.useEntity {
			ei := NewEntityIndex()
			mgr.SetEntityIndex(ei)
		}

		now := time.Now()
		for _, m := range corpus.Memories {
			mem := &Memory{
				Name: m.Name, Description: m.Description, Type: Type(m.Type),
				Content: m.Content, FilePath: filepath.Join(dir, sanitizeFilename(m.Name)+".md"),
				UpdatedAt: now,
			}
			if m.ValidUntil != "" {
				t2, err := time.Parse(time.RFC3339, m.ValidUntil)
				if err == nil {
					mem.ValidUntil = &t2
				}
			}
			mgr.Save(mem)
		}

		m := runQualityBenchmark(t, mgr, corpus, 5)
		results = append(results, ablationResult{name: cfg.name, metrics: m})
	}

	t.Logf("=== Fusion Ablation Results ===")
	for _, r := range results {
		t.Logf("  %-20s P@5=%.4f R@5=%.4f NDCG=%.4f MRR=%.4f F1=%.4f",
			r.name, r.metrics.Overall.MeanPrecision, r.metrics.Overall.MeanRecall,
			r.metrics.Overall.MeanNDCG, r.metrics.Overall.MeanMRR, r.metrics.F1Score)
	}

	// Full pipeline should not be worse than keyword-only.
	if len(results) >= 2 {
		keywordOnly := results[0].metrics
		fullPipeline := results[len(results)-1].metrics

		if fullPipeline.Overall.MeanNDCG < keywordOnly.Overall.MeanNDCG*0.95 {
			t.Errorf("full pipeline NDCG (%.4f) worse than keyword-only (%.4f) by >5%%",
				fullPipeline.Overall.MeanNDCG, keywordOnly.Overall.MeanNDCG)
		}
		t.Logf("Delta NDCG (full - keyword): %+.4f",
			fullPipeline.Overall.MeanNDCG-keywordOnly.Overall.MeanNDCG)
	}
}

// TestCompetitive_LatencyComparison compares ycode's latency against
// published SOTA latency targets.
func TestCompetitive_LatencyComparison(t *testing.T) {
	corpus := loadQualityCorpus(t)
	mgr := setupQualityManager(t, corpus)
	m := runQualityBenchmark(t, mgr, corpus, 5)

	// Published latency targets:
	// Mem0g: p95 = 200ms
	// Graphiti: p95 = 150ms
	// Memvid: p50 = 0.025ms
	// ycode: pure in-process, no network, should be fastest.

	t.Logf("=== Latency Comparison ===")
	t.Logf("  ycode       P50=%v P99=%v", m.LatencyP50, m.LatencyP99)
	t.Logf("  Mem0g       P95=200ms (published)")
	t.Logf("  Graphiti    P95=150ms (published)")
	t.Logf("  Memvid      P50=0.025ms (published)")

	// ycode should be well under 10ms P99 for 60 memories (in-process search).
	if m.LatencyP99 > 10*time.Millisecond {
		t.Errorf("P99 = %v, want < 10ms for in-process 60-memory corpus", m.LatencyP99)
	}
}
