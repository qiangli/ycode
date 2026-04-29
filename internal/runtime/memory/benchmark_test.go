package memory

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Memory Retrieval Quality Benchmark
//
// Measures retrieval quality metrics: Precision@K, Recall@K, NDCG@K, MRR.
// Uses a fixed dataset with known-relevant results to quantify improvements
// from RRF fusion, value scoring, entity linking, and temporal validity.
// ---------------------------------------------------------------------------

// BenchmarkDataset defines a test corpus of memories and queries with ground truth.
type BenchmarkDataset struct {
	Memories []*Memory
	Queries  []BenchmarkQuery
}

// BenchmarkQuery defines a query with its expected relevant memory names.
type BenchmarkQuery struct {
	Query    string
	Relevant []string // names of memories that should be retrieved
}

// RetrievalMetrics holds computed quality metrics for a single query.
type RetrievalMetrics struct {
	PrecisionAtK float64 // fraction of top-K results that are relevant
	RecallAtK    float64 // fraction of relevant results in top-K
	NDCG         float64 // normalized discounted cumulative gain
	MRR          float64 // reciprocal rank of first relevant result
}

// AggregateMetrics holds averages across all queries.
type AggregateMetrics struct {
	MeanPrecision float64
	MeanRecall    float64
	MeanNDCG      float64
	MeanMRR       float64
	QueryCount    int
}

func (a AggregateMetrics) String() string {
	return fmt.Sprintf("P@K=%.4f R@K=%.4f NDCG=%.4f MRR=%.4f (n=%d)",
		a.MeanPrecision, a.MeanRecall, a.MeanNDCG, a.MeanMRR, a.QueryCount)
}

// buildTestDataset creates a realistic memory corpus with diverse types.
func buildTestDataset() BenchmarkDataset {
	now := time.Now()
	day := 24 * time.Hour

	memories := []*Memory{
		// Auth cluster
		{Name: "auth-config", Description: "OAuth2 authentication configuration", Type: TypeProject,
			Content: "We use OAuth2 with PKCE flow. Provider is Auth0. Callback URL is /api/auth/callback.", UpdatedAt: now},
		{Name: "auth-flow", Description: "Authentication flow documentation", Type: TypeReference,
			Content: "Login flow: user clicks login → redirect to Auth0 → callback → session created.", UpdatedAt: now.Add(-5 * day)},
		{Name: "auth-bug", Description: "Fix for token refresh race condition", Type: TypeFeedback,
			Content: "Token refresh had a race condition when multiple tabs were open. Fixed by adding a mutex.", UpdatedAt: now.Add(-10 * day)},

		// Deploy cluster
		{Name: "deploy-config", Description: "Production deployment configuration", Type: TypeProject,
			Content: "Deploy target: staging-3. Use make deploy HOST=staging PORT=58080. Rolling updates enabled.", UpdatedAt: now},
		{Name: "deploy-rollback", Description: "Rollback procedure for failed deployments", Type: TypeProcedural,
			Content: "If deploy fails: 1) Check logs 2) Run make rollback 3) Notify #ops channel.", UpdatedAt: now.Add(-3 * day)},
		{Name: "deploy-freeze", Description: "Merge freeze for release branch", Type: TypeProject,
			Content: "Merge freeze starts 2026-05-01 for mobile release. No non-critical PRs after that date.", UpdatedAt: now},

		// Memory system cluster
		{Name: "memory-architecture", Description: "Five-layer memory system design", Type: TypeReference,
			Content: "L1 working, L2 episodic, L3 compaction, L4 procedural, L5 persistent. Vector + Bleve + keyword search.", UpdatedAt: now},
		{Name: "memory-scoring", Description: "Composite scoring algorithm details", Type: TypeReference,
			Content: "Score = 0.5*semantic + 0.3*recency + 0.2*importance. 30-day half-life for recency decay.", UpdatedAt: now.Add(-7 * day)},

		// User preferences
		{Name: "user-editor", Description: "User prefers vim as code editor", Type: TypeUser,
			Content: "User uses neovim with custom config. Prefers modal editing. Has Go, Rust, TypeScript LSPs configured.", UpdatedAt: now.Add(-30 * day)},
		{Name: "user-testing", Description: "User prefers TDD workflow", Type: TypeFeedback,
			Content: "Always write tests first. Integration tests should hit real database, not mocks.", UpdatedAt: now.Add(-20 * day)},

		// API cluster
		{Name: "api-endpoints", Description: "REST API endpoint documentation", Type: TypeReference,
			Content: "GET /api/health, POST /api/auth/login, GET /api/memories, POST /api/memories/search.", UpdatedAt: now},
		{Name: "api-rate-limit", Description: "API rate limiting configuration", Type: TypeProject,
			Content: "Rate limit: 100 req/min per user. Burst: 20. Uses token bucket algorithm. Redis-backed.", UpdatedAt: now.Add(-2 * day)},

		// Database
		{Name: "db-schema", Description: "Database schema for sessions table", Type: TypeReference,
			Content: "Sessions table: id, user_id, created_at, updated_at, metadata JSONB. Index on user_id.", UpdatedAt: now.Add(-15 * day)},
		{Name: "db-migration", Description: "Pending database migration for v2", Type: TypeProject,
			Content: "Add columns: value_score FLOAT, access_count INT, entities TEXT[]. Migration 0042.", UpdatedAt: now},

		// Superseded memory (temporal validity test)
		{Name: "old-deploy-target", Description: "Old deployment target (superseded)", Type: TypeProject,
			Content: "Deploy target: staging-2. This has been superseded.", UpdatedAt: now.Add(-60 * day),
			ValidUntil: timePtr(now.Add(-30 * day)), SupersededBy: "deploy-config"},
	}

	queries := []BenchmarkQuery{
		{Query: "how to authenticate users", Relevant: []string{"auth-config", "auth-flow"}},
		{Query: "deploy to production", Relevant: []string{"deploy-config", "deploy-rollback"}},
		{Query: "memory system architecture", Relevant: []string{"memory-architecture", "memory-scoring"}},
		{Query: "API rate limiting", Relevant: []string{"api-rate-limit", "api-endpoints"}},
		{Query: "database schema migration", Relevant: []string{"db-schema", "db-migration"}},
		{Query: "user testing preferences", Relevant: []string{"user-testing", "user-editor"}},
		{Query: "deployment freeze schedule", Relevant: []string{"deploy-freeze", "deploy-config"}},
		{Query: "token refresh bug", Relevant: []string{"auth-bug", "auth-config"}},
	}

	return BenchmarkDataset{Memories: memories, Queries: queries}
}

// computeMetrics calculates retrieval metrics for a single query.
func computeMetrics(results []SearchResult, relevant []string, k int) RetrievalMetrics {
	relevantSet := make(map[string]bool, len(relevant))
	for _, r := range relevant {
		relevantSet[r] = true
	}

	if k > len(results) {
		k = len(results)
	}

	// Precision@K and Recall@K
	hits := 0
	for i := 0; i < k; i++ {
		if relevantSet[results[i].Memory.Name] {
			hits++
		}
	}

	precisionAtK := 0.0
	if k > 0 {
		precisionAtK = float64(hits) / float64(k)
	}

	recallAtK := 0.0
	if len(relevant) > 0 {
		recallAtK = float64(hits) / float64(len(relevant))
	}

	// MRR — reciprocal rank of the first relevant result.
	mrr := 0.0
	for i := 0; i < k; i++ {
		if relevantSet[results[i].Memory.Name] {
			mrr = 1.0 / float64(i+1)
			break
		}
	}

	// NDCG@K
	dcg := 0.0
	for i := 0; i < k; i++ {
		if relevantSet[results[i].Memory.Name] {
			dcg += 1.0 / math.Log2(float64(i+2)) // log2(rank+1), rank is 1-indexed
		}
	}

	// Ideal DCG — all relevant results at top positions.
	idealK := len(relevant)
	if idealK > k {
		idealK = k
	}
	idcg := 0.0
	for i := 0; i < idealK; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}

	ndcg := 0.0
	if idcg > 0 {
		ndcg = dcg / idcg
	}

	return RetrievalMetrics{
		PrecisionAtK: precisionAtK,
		RecallAtK:    recallAtK,
		NDCG:         ndcg,
		MRR:          mrr,
	}
}

// runBenchmark evaluates retrieval quality across all queries.
func runBenchmark(t *testing.T, mgr *Manager, dataset BenchmarkDataset, k int) AggregateMetrics {
	t.Helper()

	var totalP, totalR, totalNDCG, totalMRR float64
	for _, q := range dataset.Queries {
		results, err := mgr.Recall(q.Query, k)
		if err != nil {
			t.Errorf("recall %q: %v", q.Query, err)
			continue
		}

		m := computeMetrics(results, q.Relevant, k)
		totalP += m.PrecisionAtK
		totalR += m.RecallAtK
		totalNDCG += m.NDCG
		totalMRR += m.MRR
	}

	n := float64(len(dataset.Queries))
	return AggregateMetrics{
		MeanPrecision: totalP / n,
		MeanRecall:    totalR / n,
		MeanNDCG:      totalNDCG / n,
		MeanMRR:       totalMRR / n,
		QueryCount:    len(dataset.Queries),
	}
}

// ---------------------------------------------------------------------------
// E2E Benchmark Tests
// ---------------------------------------------------------------------------

// TestBenchmark_BaselineRetrieval measures retrieval quality with keyword-only search.
func TestBenchmark_BaselineRetrieval(t *testing.T) {
	dataset := buildTestDataset()
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	for _, mem := range dataset.Memories {
		mem.FilePath = filepath.Join(dir, sanitizeFilename(mem.Name)+".md")
		if err := mgr.Save(mem); err != nil {
			t.Fatalf("save %s: %v", mem.Name, err)
		}
	}

	metrics := runBenchmark(t, mgr, dataset, 5)
	t.Logf("Baseline (keyword-only): %s", metrics)

	// Keyword search should achieve non-trivial results.
	if metrics.MeanPrecision < 0.1 {
		t.Errorf("baseline precision too low: %f", metrics.MeanPrecision)
	}
	if metrics.MeanMRR < 0.1 {
		t.Errorf("baseline MRR too low: %f", metrics.MeanMRR)
	}
}

// TestBenchmark_WithEntityIndex measures improvement when entity linking is enabled.
func TestBenchmark_WithEntityIndex(t *testing.T) {
	dataset := buildTestDataset()

	// Baseline without entity index.
	dir1 := t.TempDir()
	mgr1, _ := NewManager(dir1)
	for _, mem := range dataset.Memories {
		cp := *mem
		cp.FilePath = filepath.Join(dir1, sanitizeFilename(cp.Name)+".md")
		mgr1.Save(&cp)
	}
	baseline := runBenchmark(t, mgr1, dataset, 5)

	// With entity index.
	dir2 := t.TempDir()
	mgr2, _ := NewManager(dir2)
	ei := NewEntityIndex()
	mgr2.SetEntityIndex(ei)
	for _, mem := range dataset.Memories {
		cp := *mem
		cp.FilePath = filepath.Join(dir2, sanitizeFilename(cp.Name)+".md")
		mgr2.Save(&cp)
	}
	withEntities := runBenchmark(t, mgr2, dataset, 5)

	t.Logf("Baseline:       %s", baseline)
	t.Logf("With entities:  %s", withEntities)

	// Entity index should not degrade results.
	if withEntities.MeanPrecision < baseline.MeanPrecision*0.9 {
		t.Errorf("entity index degraded precision: %f -> %f", baseline.MeanPrecision, withEntities.MeanPrecision)
	}
}

// TestBenchmark_ValueScoringBoostsRecalledMemories verifies that memories with
// higher value scores rank above identical-content memories with default scores.
func TestBenchmark_ValueScoringBoostsRecalledMemories(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Two similar memories: one with high value, one with default.
	highValue := &Memory{
		Name:        "deploy-high",
		Description: "deployment config (frequently accessed)",
		Type:        TypeProject,
		Content:     "Deploy to staging-3 with rolling updates.",
		ValueScore:  0.95,
		AccessCount: 10,
		UpdatedAt:   time.Now(),
	}
	lowValue := &Memory{
		Name:        "deploy-low",
		Description: "deployment config (rarely accessed)",
		Type:        TypeProject,
		Content:     "Deploy to staging-3 with rolling updates.",
		Importance:  0.5,
		UpdatedAt:   time.Now(),
	}

	mgr.Save(highValue)
	mgr.Save(lowValue)

	results, _ := mgr.Recall("deployment config", 2)
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// High-value memory should rank first.
	if results[0].Memory.Name != "deploy-high" {
		t.Errorf("expected deploy-high first (value=0.95), got %q", results[0].Memory.Name)
	}
}

// TestBenchmark_TemporalValidityPenalty verifies that expired memories rank lower.
func TestBenchmark_TemporalValidityPenalty(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	past := time.Now().Add(-time.Hour)

	expired := &Memory{
		Name:        "old-target",
		Description: "deployment target (expired)",
		Type:        TypeProject,
		Content:     "Deploy to staging-2.",
		UpdatedAt:   time.Now(),
		ValidUntil:  &past,
	}
	current := &Memory{
		Name:        "current-target",
		Description: "deployment target (current)",
		Type:        TypeProject,
		Content:     "Deploy to staging-3.",
		UpdatedAt:   time.Now(),
	}

	mgr.Save(expired)
	mgr.Save(current)

	results, _ := mgr.Recall("deployment target", 2)
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Current memory should rank above expired.
	if results[0].Memory.Name != "current-target" {
		t.Errorf("expected current-target first, got %q (expired should be penalized)", results[0].Memory.Name)
	}
}

// TestBenchmark_RRFFusionBoostsMultiBackendHits verifies that memories
// appearing in multiple search backends rank higher than single-backend hits.
func TestBenchmark_RRFFusionBoostsMultiBackendHits(t *testing.T) {
	// This tests the RRF algorithm directly with known inputs.
	results := map[string][]SearchResult{
		"keyword": {
			{Memory: &Memory{Name: "deploy-config", Description: "deployment configuration"}, Score: 6.0},
			{Memory: &Memory{Name: "deploy-freeze"}, Score: 3.0},
		},
		"entity": {
			{Memory: &Memory{Name: "deploy-config", Description: "deployment configuration"}, Score: 2.0},
			{Memory: &Memory{Name: "api-endpoints"}, Score: 1.0},
		},
	}

	fused := ReciprocalRankFusion(results, 60)

	// deploy-config appears in both backends, should rank first.
	if len(fused) == 0 {
		t.Fatal("expected fused results")
	}
	if fused[0].Memory.Name != "deploy-config" {
		t.Errorf("multi-backend hit should rank first, got %q", fused[0].Memory.Name)
	}
}

// TestBenchmark_MMRDiversifiesResults verifies MMR promotes topically different results.
func TestBenchmark_MMRDiversifiesResults(t *testing.T) {
	results := []SearchResult{
		{Memory: &Memory{Name: "a", Description: "deploy config production env", Content: "deploy to prod"}, Score: 1.0},
		{Memory: &Memory{Name: "b", Description: "deploy config staging env", Content: "deploy to staging"}, Score: 0.95},
		{Memory: &Memory{Name: "c", Description: "authentication OAuth2 flow", Content: "login with auth0"}, Score: 0.90},
		{Memory: &Memory{Name: "d", Description: "deploy rollback procedure", Content: "rollback deploy"}, Score: 0.85},
	}

	// With lambda=0.5 (balanced), auth should be promoted for diversity.
	diverse := MMRRerank(results, 0.5, 4)

	// "c" (auth) should appear before "b" or "d" (both deploy-related) because
	// after selecting "a" (deploy), the most diverse next pick is "c" (auth).
	authIdx := -1
	for i, r := range diverse {
		if r.Memory.Name == "c" {
			authIdx = i
			break
		}
	}
	if authIdx != 1 {
		t.Errorf("expected auth topic at position 1 for diversity, got position %d", authIdx)
		for i, r := range diverse {
			t.Logf("  %d: %s (score=%f)", i, r.Memory.Name, r.Score)
		}
	}
}

// TestBenchmark_ConsolidationReducesRedundancy verifies the dreamer reduces memory count.
func TestBenchmark_ConsolidationReducesRedundancy(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Create redundant memories with identical descriptions.
	for i := 0; i < 3; i++ {
		mgr.Save(&Memory{
			Name:        fmt.Sprintf("deploy-redundant-%d", i),
			Description: "deployment configuration for production environment",
			Type:        TypeProject,
			Content:     fmt.Sprintf("Deploy instructions version %d", i),
		})
	}

	countBefore, _ := mgr.All()
	if len(countBefore) != 3 {
		t.Fatalf("expected 3 memories before consolidation, got %d", len(countBefore))
	}

	d := NewDreamer(mgr, true)
	if err := d.consolidate(); err != nil {
		t.Fatalf("consolidate: %v", err)
	}

	countAfter, _ := mgr.All()
	if len(countAfter) >= len(countBefore) {
		t.Errorf("consolidation should reduce memory count: before=%d, after=%d", len(countBefore), len(countAfter))
	}
	t.Logf("Consolidation: %d -> %d memories", len(countBefore), len(countAfter))
}

// TestBenchmark_RewardPropagationEffectiveness verifies that reward feedback
// changes ranking over multiple rounds.
func TestBenchmark_RewardPropagationEffectiveness(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Two equally-described memories.
	mem1 := &Memory{
		Name:        "approach-a",
		Description: "testing approach",
		Type:        TypeFeedback,
		Content:     "Use integration tests with real database.",
		Importance:  0.5,
		UpdatedAt:   time.Now(),
	}
	mem2 := &Memory{
		Name:        "approach-b",
		Description: "testing approach",
		Type:        TypeFeedback,
		Content:     "Use unit tests with mocks.",
		Importance:  0.5,
		UpdatedAt:   time.Now(),
	}

	mgr.Save(mem1)
	mgr.Save(mem2)

	// Give positive feedback to approach-a.
	for i := 0; i < 5; i++ {
		PropagateReward(mem1, 1.0, DefaultRewardAlpha)
	}
	mgr.Save(mem1)

	// Give negative feedback to approach-b.
	for i := 0; i < 5; i++ {
		PropagateReward(mem2, 0.1, DefaultRewardAlpha)
	}
	mgr.Save(mem2)

	results, _ := mgr.Recall("testing approach", 2)
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Memory.Name != "approach-a" {
		t.Errorf("positively-rewarded memory should rank first, got %q", results[0].Memory.Name)
	}

	t.Logf("After reward: approach-a value=%.4f, approach-b value=%.4f",
		mem1.EffectiveValue(), mem2.EffectiveValue())
}

// TestBenchmark_TurnInjectionRelevance verifies turn injector returns
// contextually appropriate memories.
func TestBenchmark_TurnInjectionRelevance(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	mgr.Save(&Memory{
		Name:        "auth-info",
		Description: "authentication configuration",
		Type:        TypeProject,
		Content:     "Use OAuth2 with Auth0 provider.",
		FilePath:    filepath.Join(dir, "auth-info.md"),
		UpdatedAt:   time.Now(),
	})
	mgr.Save(&Memory{
		Name:        "unrelated",
		Description: "weather forecast for tomorrow",
		Type:        TypeProject,
		Content:     "Sunny with a chance of rain.",
		FilePath:    filepath.Join(dir, "unrelated.md"),
		UpdatedAt:   time.Now(),
	})

	ti := NewTurnInjector(mgr, 1500)
	result := ti.InjectForTurn(context.Background(), "how does OAuth authentication work?")

	if !strings.Contains(result, "auth-info") {
		t.Error("turn injection should include auth-related memory")
	}
	if strings.Contains(result, "weather") {
		t.Error("turn injection should not include unrelated memory")
	}
}

// TestBenchmark_ProfileRoundtrip verifies profile persistence and retrieval.
func TestBenchmark_ProfileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	p := NewUserProfile()
	p.Update("basic_info.name", "Alice")
	p.Update("basic_info.role", "senior engineer")
	p.Update("preferences.editor", "neovim")
	p.Update("expertise", "Go")
	p.Update("expertise", "distributed systems")
	p.Update("work_patterns", "TDD")

	if err := p.Save(store); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	loaded, err := LoadProfile(store)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	// All fields should survive roundtrip.
	checks := []struct {
		field string
		got   string
		want  string
	}{
		{"name", loaded.BasicInfo["name"], "Alice"},
		{"role", loaded.BasicInfo["role"], "senior engineer"},
		{"editor", loaded.Preferences["editor"], "neovim"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("profile.%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if len(loaded.Expertise) != 2 {
		t.Errorf("expertise count = %d, want 2", len(loaded.Expertise))
	}
}

// TestBenchmark_OverallQualityMinimums sets minimum quality thresholds.
// These serve as regression gates — future changes must not degrade below these.
func TestBenchmark_OverallQualityMinimums(t *testing.T) {
	dataset := buildTestDataset()
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	ei := NewEntityIndex()
	mgr.SetEntityIndex(ei)

	for _, mem := range dataset.Memories {
		cp := *mem
		cp.FilePath = filepath.Join(dir, sanitizeFilename(cp.Name)+".md")
		mgr.Save(&cp)
	}

	metrics := runBenchmark(t, mgr, dataset, 5)
	t.Logf("Full system: %s", metrics)

	// Minimum quality thresholds (established from initial measurements).
	if metrics.MeanPrecision < 0.15 {
		t.Errorf("MeanPrecision %f below minimum 0.15", metrics.MeanPrecision)
	}
	if metrics.MeanRecall < 0.15 {
		t.Errorf("MeanRecall %f below minimum 0.15", metrics.MeanRecall)
	}
	if metrics.MeanNDCG < 0.15 {
		t.Errorf("MeanNDCG %f below minimum 0.15", metrics.MeanNDCG)
	}
	if metrics.MeanMRR < 0.20 {
		t.Errorf("MeanMRR %f below minimum 0.20", metrics.MeanMRR)
	}
}

// timePtr is a helper to create *time.Time for test data.
func timePtr(t time.Time) *time.Time {
	return &t
}
