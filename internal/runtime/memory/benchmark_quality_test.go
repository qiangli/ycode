package memory

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Quality Benchmark — Larger Corpus (60 memories, 30 queries)
//
// Extends the baseline benchmark with per-category breakdowns, adversarial
// false-positive detection, F1 score, and latency tracking.
// ---------------------------------------------------------------------------

// qualityCorpus is the deserialized benchmark corpus from testdata.
type qualityCorpus struct {
	Memories []qualityMemory `json:"memories"`
	Queries  []qualityQuery  `json:"queries"`
}

type qualityMemory struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Content      string `json:"content"`
	ValidUntil   string `json:"valid_until,omitempty"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

type qualityQuery struct {
	Query      string   `json:"query"`
	Relevant   []string `json:"relevant"`
	Irrelevant []string `json:"irrelevant,omitempty"`
	Category   string   `json:"category"`
}

// qualityMetrics extends aggregate metrics with per-category detail.
type qualityMetrics struct {
	Overall     AggregateMetrics
	F1Score     float64
	PerCategory map[string]AggregateMetrics
	// Adversarial: fraction of adversarial queries returning irrelevant results in top-K.
	AdversarialFPR float64
	LatencyP50     time.Duration
	LatencyP99     time.Duration
}

func loadQualityCorpus(t *testing.T) qualityCorpus {
	t.Helper()
	data, err := os.ReadFile("testdata/benchmark_corpus.json")
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	var corpus qualityCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return corpus
}

func setupQualityManager(t *testing.T, corpus qualityCorpus) *Manager {
	t.Helper()
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	ei := NewEntityIndex()
	mgr.SetEntityIndex(ei)

	now := time.Now()
	for _, m := range corpus.Memories {
		mem := &Memory{
			Name:        m.Name,
			Description: m.Description,
			Type:        Type(m.Type),
			Content:     m.Content,
			FilePath:    filepath.Join(dir, sanitizeFilename(m.Name)+".md"),
			UpdatedAt:   now,
		}
		if m.ValidUntil != "" {
			t2, err := time.Parse(time.RFC3339, m.ValidUntil)
			if err == nil {
				mem.ValidUntil = &t2
			}
		}
		if m.SupersededBy != "" {
			mem.SupersededBy = m.SupersededBy
		}
		if err := mgr.Save(mem); err != nil {
			t.Fatalf("save %s: %v", m.Name, err)
		}
	}
	return mgr
}

func runQualityBenchmark(t *testing.T, mgr *Manager, corpus qualityCorpus, k int) qualityMetrics {
	t.Helper()

	categoryTotals := make(map[string]*[4]float64) // P, R, NDCG, MRR sums
	categoryCounts := make(map[string]int)
	var totalP, totalR, totalNDCG, totalMRR float64

	adversarialTotal := 0
	adversarialFP := 0

	var latencies []time.Duration

	for _, q := range corpus.Queries {
		start := time.Now()
		results, err := mgr.Recall(q.Query, k)
		elapsed := time.Since(start)
		latencies = append(latencies, elapsed)

		if err != nil {
			t.Errorf("recall %q: %v", q.Query, err)
			continue
		}

		m := computeMetrics(results, q.Relevant, k)
		totalP += m.PrecisionAtK
		totalR += m.RecallAtK
		totalNDCG += m.NDCG
		totalMRR += m.MRR

		// Per-category.
		if _, ok := categoryTotals[q.Category]; !ok {
			categoryTotals[q.Category] = &[4]float64{}
		}
		categoryTotals[q.Category][0] += m.PrecisionAtK
		categoryTotals[q.Category][1] += m.RecallAtK
		categoryTotals[q.Category][2] += m.NDCG
		categoryTotals[q.Category][3] += m.MRR
		categoryCounts[q.Category]++

		// Adversarial: check if irrelevant results appear in top-K.
		if q.Category == "adversarial" || len(q.Irrelevant) > 0 {
			irrelevantSet := make(map[string]bool, len(q.Irrelevant))
			for _, ir := range q.Irrelevant {
				irrelevantSet[ir] = true
			}
			adversarialTotal++
			for i := range min(k, len(results)) {
				if irrelevantSet[results[i].Memory.Name] {
					adversarialFP++
					break
				}
			}
		}
	}

	n := float64(len(corpus.Queries))
	meanP := totalP / n
	meanR := totalR / n

	// F1 from mean P and R.
	f1 := 0.0
	if meanP+meanR > 0 {
		f1 = 2 * meanP * meanR / (meanP + meanR)
	}

	// Per-category aggregates.
	perCat := make(map[string]AggregateMetrics)
	for cat, sums := range categoryTotals {
		cn := float64(categoryCounts[cat])
		perCat[cat] = AggregateMetrics{
			MeanPrecision: sums[0] / cn,
			MeanRecall:    sums[1] / cn,
			MeanNDCG:      sums[2] / cn,
			MeanMRR:       sums[3] / cn,
			QueryCount:    categoryCounts[cat],
		}
	}

	// Adversarial FPR.
	fpr := 0.0
	if adversarialTotal > 0 {
		fpr = float64(adversarialFP) / float64(adversarialTotal)
	}

	// Latency percentiles.
	slices.Sort(latencies)
	p50 := latencies[len(latencies)/2]
	p99Idx := int(math.Ceil(float64(len(latencies))*0.99)) - 1
	if p99Idx >= len(latencies) {
		p99Idx = len(latencies) - 1
	}
	p99 := latencies[p99Idx]

	return qualityMetrics{
		Overall: AggregateMetrics{
			MeanPrecision: meanP,
			MeanRecall:    meanR,
			MeanNDCG:      totalNDCG / n,
			MeanMRR:       totalMRR / n,
			QueryCount:    len(corpus.Queries),
		},
		F1Score:        f1,
		PerCategory:    perCat,
		AdversarialFPR: fpr,
		LatencyP50:     p50,
		LatencyP99:     p99,
	}
}

// TestBenchmark_QualityAtK5 runs the full 60-memory/30-query corpus at K=5.
func TestBenchmark_QualityAtK5(t *testing.T) {
	corpus := loadQualityCorpus(t)
	mgr := setupQualityManager(t, corpus)
	m := runQualityBenchmark(t, mgr, corpus, 5)

	t.Logf("Quality@5: %s F1=%.4f", m.Overall, m.F1Score)
	t.Logf("Latency: P50=%v P99=%v", m.LatencyP50, m.LatencyP99)
	t.Logf("Adversarial FPR: %.4f", m.AdversarialFPR)

	// Overall quality gates — calibrated at 80% of measured values
	// (P@5=0.17, R@5=0.54, NDCG=0.50, MRR=0.57, F1=0.26 as of 2026-05-01).
	if m.Overall.MeanPrecision < 0.13 {
		t.Errorf("overall P@5 = %.4f, want >= 0.13", m.Overall.MeanPrecision)
	}
	if m.Overall.MeanMRR < 0.45 {
		t.Errorf("overall MRR = %.4f, want >= 0.45", m.Overall.MeanMRR)
	}
	if m.F1Score < 0.20 {
		t.Errorf("F1@5 = %.4f, want >= 0.20", m.F1Score)
	}
}

// TestBenchmark_QualityByCategory reports per-category breakdown.
func TestBenchmark_QualityByCategory(t *testing.T) {
	corpus := loadQualityCorpus(t)
	mgr := setupQualityManager(t, corpus)
	m := runQualityBenchmark(t, mgr, corpus, 5)

	for _, cat := range []string{"single_hop", "multi_hop", "temporal", "adversarial"} {
		cm, ok := m.PerCategory[cat]
		if !ok {
			t.Errorf("missing category: %s", cat)
			continue
		}
		t.Logf("  %s: %s", cat, cm)
	}

	// Single-hop should be strongest.
	if sh, ok := m.PerCategory["single_hop"]; ok {
		if sh.MeanMRR < 0.40 {
			t.Errorf("single_hop MRR = %.4f, want >= 0.40", sh.MeanMRR)
		}
	}
}

// TestBenchmark_QualityLatency asserts recall latency is within bounds.
func TestBenchmark_QualityLatency(t *testing.T) {
	corpus := loadQualityCorpus(t)
	mgr := setupQualityManager(t, corpus)
	m := runQualityBenchmark(t, mgr, corpus, 5)

	t.Logf("Latency: P50=%v P99=%v", m.LatencyP50, m.LatencyP99)

	if m.LatencyP99 > 50*time.Millisecond {
		t.Errorf("P99 latency = %v, want < 50ms for 60-memory corpus", m.LatencyP99)
	}
}

// TestBenchmark_QualityAdversarialRejection checks that adversarial queries
// don't surface obviously irrelevant results.
func TestBenchmark_QualityAdversarialRejection(t *testing.T) {
	corpus := loadQualityCorpus(t)
	mgr := setupQualityManager(t, corpus)
	m := runQualityBenchmark(t, mgr, corpus, 5)

	t.Logf("Adversarial FPR: %.4f", m.AdversarialFPR)

	// With keyword-based search, adversarial queries about unrelated topics
	// should not match domain-specific memories. Allow up to 30% FPR since
	// keyword matching can produce coincidental hits.
	if m.AdversarialFPR > 0.30 {
		t.Errorf("adversarial FPR = %.4f, want <= 0.30", m.AdversarialFPR)
	}
}

// BenchmarkRecall_60Memories measures recall throughput on the full corpus.
func BenchmarkRecall_60Memories(b *testing.B) {
	corpus := loadQualityCorpusBench(b)
	dir := b.TempDir()
	mgr, _ := NewManager(dir)
	ei := NewEntityIndex()
	mgr.SetEntityIndex(ei)

	for _, m := range corpus.Memories {
		mem := &Memory{
			Name: m.Name, Description: m.Description, Type: Type(m.Type),
			Content: m.Content, FilePath: filepath.Join(dir, m.Name+".md"),
			UpdatedAt: time.Now(),
		}
		mgr.Save(mem)
	}

	queries := make([]string, len(corpus.Queries))
	for i, q := range corpus.Queries {
		queries[i] = q.Query
	}

	b.ResetTimer()
	for i := range b.N {
		mgr.Recall(queries[i%len(queries)], 5)
	}
}

// BenchmarkRecall_200Memories stress-tests with a larger corpus.
func BenchmarkRecall_200Memories(b *testing.B) {
	corpus := loadQualityCorpusBench(b)
	dir := b.TempDir()
	mgr, _ := NewManager(dir)

	// Multiply the corpus to reach ~200 memories.
	for rep := range 4 {
		for _, m := range corpus.Memories {
			mem := &Memory{
				Name:        m.Name + "-" + string(rune('a'+rep)),
				Description: m.Description,
				Type:        Type(m.Type),
				Content:     m.Content,
				FilePath:    filepath.Join(dir, m.Name+"-"+string(rune('a'+rep))+".md"),
				UpdatedAt:   time.Now(),
			}
			mgr.Save(mem)
		}
	}

	queries := make([]string, len(corpus.Queries))
	for i, q := range corpus.Queries {
		queries[i] = q.Query
	}

	b.ResetTimer()
	for i := range b.N {
		mgr.Recall(queries[i%len(queries)], 5)
	}
}

func loadQualityCorpusBench(b *testing.B) qualityCorpus {
	b.Helper()
	data, err := os.ReadFile("testdata/benchmark_corpus.json")
	if err != nil {
		b.Fatalf("load corpus: %v", err)
	}
	var corpus qualityCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		b.Fatalf("parse corpus: %v", err)
	}
	return corpus
}
