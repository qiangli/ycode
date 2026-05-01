// Package contract provides deterministic validation tests for ycode's
// infrastructure. These tests run without LLM, network, or containers.
package contract

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

// =============================================================================
// Memory: Validate retrieval quality, fusion, temporal ordering, value scoring,
// consolidation, turn injection, cross-scope recall, and reward convergence.
// =============================================================================

// buildContractDataset creates a compact memory corpus with known-relevant pairs
// for regression testing. Mirrors the benchmark dataset but lives in the
// contract package for independent threshold enforcement.
func buildContractDataset() ([]*memory.Memory, []contractQuery) {
	now := time.Now()
	day := 24 * time.Hour

	memories := []*memory.Memory{
		// Auth cluster
		{Name: "auth-config", Description: "OAuth2 authentication configuration", Type: memory.TypeProject,
			Content: "We use OAuth2 with PKCE flow. Provider is Auth0. Callback URL is /api/auth/callback.", UpdatedAt: now},
		{Name: "auth-flow", Description: "Authentication flow documentation", Type: memory.TypeReference,
			Content: "Login flow: user clicks login → redirect to Auth0 → callback → session created.", UpdatedAt: now.Add(-5 * day)},
		{Name: "auth-bug", Description: "Fix for token refresh race condition", Type: memory.TypeFeedback,
			Content: "Token refresh had a race condition when multiple tabs were open. Fixed by adding a mutex.", UpdatedAt: now.Add(-10 * day)},

		// Deploy cluster
		{Name: "deploy-config", Description: "Production deployment configuration", Type: memory.TypeProject,
			Content: "Deploy target: staging-3. Use make deploy HOST=staging PORT=58080. Rolling updates enabled.", UpdatedAt: now},
		{Name: "deploy-rollback", Description: "Rollback procedure for failed deployments", Type: memory.TypeProcedural,
			Content: "If deploy fails: 1) Check logs 2) Run make rollback 3) Notify #ops channel.", UpdatedAt: now.Add(-3 * day)},
		{Name: "deploy-freeze", Description: "Merge freeze for release branch", Type: memory.TypeProject,
			Content: "Merge freeze starts 2026-05-01 for mobile release. No non-critical PRs after that date.", UpdatedAt: now},

		// Memory system cluster
		{Name: "memory-architecture", Description: "Five-layer memory system design", Type: memory.TypeReference,
			Content: "L1 working, L2 episodic, L3 compaction, L4 procedural, L5 persistent. Vector + Bleve + keyword search.", UpdatedAt: now},
		{Name: "memory-scoring", Description: "Composite scoring algorithm details", Type: memory.TypeReference,
			Content: "Score = 0.5*semantic + 0.3*recency + 0.2*importance. 30-day half-life for recency decay.", UpdatedAt: now.Add(-7 * day)},

		// User preferences
		{Name: "user-editor", Description: "User prefers vim as code editor", Type: memory.TypeUser,
			Content: "User uses neovim with custom config. Prefers modal editing. Has Go, Rust, TypeScript LSPs configured.", UpdatedAt: now.Add(-30 * day)},
		{Name: "user-testing", Description: "User prefers TDD workflow", Type: memory.TypeFeedback,
			Content: "Always write tests first. Integration tests should hit real database, not mocks.", UpdatedAt: now.Add(-20 * day)},

		// API cluster
		{Name: "api-endpoints", Description: "REST API endpoint documentation", Type: memory.TypeReference,
			Content: "GET /api/health, POST /api/auth/login, GET /api/memories, POST /api/memories/search.", UpdatedAt: now},
		{Name: "api-rate-limit", Description: "API rate limiting configuration", Type: memory.TypeProject,
			Content: "Rate limit: 100 req/min per user. Burst: 20. Uses token bucket algorithm. Redis-backed.", UpdatedAt: now.Add(-2 * day)},

		// Database
		{Name: "db-schema", Description: "Database schema for sessions table", Type: memory.TypeReference,
			Content: "Sessions table: id, user_id, created_at, updated_at, metadata JSONB. Index on user_id.", UpdatedAt: now.Add(-15 * day)},
		{Name: "db-migration", Description: "Pending database migration for v2", Type: memory.TypeProject,
			Content: "Add columns: value_score FLOAT, access_count INT, entities TEXT[]. Migration 0042.", UpdatedAt: now},

		// Superseded memory
		{Name: "old-deploy-target", Description: "Old deployment target (superseded)", Type: memory.TypeProject,
			Content: "Deploy target: staging-2. This has been superseded.", UpdatedAt: now.Add(-60 * day),
			ValidUntil: ptrTime(now.Add(-30 * day)), SupersededBy: "deploy-config"},
	}

	queries := []contractQuery{
		{Query: "how to authenticate users", Relevant: []string{"auth-config", "auth-flow"}},
		{Query: "deploy to production", Relevant: []string{"deploy-config", "deploy-rollback"}},
		{Query: "memory system architecture", Relevant: []string{"memory-architecture", "memory-scoring"}},
		{Query: "API rate limiting", Relevant: []string{"api-rate-limit", "api-endpoints"}},
		{Query: "database schema migration", Relevant: []string{"db-schema", "db-migration"}},
		{Query: "user testing preferences", Relevant: []string{"user-testing", "user-editor"}},
		{Query: "deployment freeze schedule", Relevant: []string{"deploy-freeze", "deploy-config"}},
		{Query: "token refresh bug", Relevant: []string{"auth-bug", "auth-config"}},
	}

	return memories, queries
}

type contractQuery struct {
	Query    string
	Relevant []string
}

func ptrTime(t time.Time) *time.Time { return &t }

// computeContractMetrics calculates P@K, R@K, NDCG@K, and MRR for a result set.
func computeContractMetrics(results []memory.SearchResult, relevant []string, k int) (precision, recall, ndcg, mrr float64) {
	relevantSet := make(map[string]bool, len(relevant))
	for _, r := range relevant {
		relevantSet[r] = true
	}
	k = min(k, len(results))

	hits := 0
	for i := 0; i < k; i++ {
		if relevantSet[results[i].Memory.Name] {
			hits++
		}
	}
	if k > 0 {
		precision = float64(hits) / float64(k)
	}
	if len(relevant) > 0 {
		recall = float64(hits) / float64(len(relevant))
	}

	// MRR
	for i := 0; i < k; i++ {
		if relevantSet[results[i].Memory.Name] {
			mrr = 1.0 / float64(i+1)
			break
		}
	}

	// NDCG
	dcg := 0.0
	for i := 0; i < k; i++ {
		if relevantSet[results[i].Memory.Name] {
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	idealK := min(len(relevant), k)
	idcg := 0.0
	for i := range idealK {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg > 0 {
		ndcg = dcg / idcg
	}
	return
}

// ---------------------------------------------------------------------------
// Test 1: Retrieval Quality Gate
// Calibrated at 80% of measured values: P@5=0.52, R@5=0.81, NDCG=0.77, MRR=0.90
// ---------------------------------------------------------------------------

func TestMemory_RetrievalQualityGate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory quality gate in short mode")
	}

	memories, queries := buildContractDataset()
	dir := t.TempDir()
	mgr, err := memory.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	ei := memory.NewEntityIndex()
	mgr.SetEntityIndex(ei)

	for _, mem := range memories {
		mem.FilePath = filepath.Join(dir, mem.Name+".md")
		if err := mgr.Save(mem); err != nil {
			t.Fatalf("save %s: %v", mem.Name, err)
		}
	}

	var totalP, totalR, totalNDCG, totalMRR float64
	for _, q := range queries {
		results, err := mgr.Recall(q.Query, 5)
		if err != nil {
			t.Errorf("recall %q: %v", q.Query, err)
			continue
		}
		p, r, n, m := computeContractMetrics(results, q.Relevant, 5)
		totalP += p
		totalR += r
		totalNDCG += n
		totalMRR += m
	}
	count := float64(len(queries))
	meanP := totalP / count
	meanR := totalR / count
	meanN := totalNDCG / count
	meanM := totalMRR / count

	t.Logf("Quality gate: P@5=%.4f R@5=%.4f NDCG=%.4f MRR=%.4f", meanP, meanR, meanN, meanM)

	// Thresholds at 80% of calibrated values (P=0.52, R=0.81, NDCG=0.77, MRR=0.90).
	if meanP < 0.40 {
		t.Errorf("P@5 = %.4f, want >= 0.40", meanP)
	}
	if meanR < 0.65 {
		t.Errorf("R@5 = %.4f, want >= 0.65", meanR)
	}
	if meanN < 0.60 {
		t.Errorf("NDCG@5 = %.4f, want >= 0.60", meanN)
	}
	if meanM < 0.70 {
		t.Errorf("MRR = %.4f, want >= 0.70", meanM)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Entity Extraction Accuracy
// ---------------------------------------------------------------------------

func TestMemory_EntityExtractionAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping entity extraction accuracy in short mode")
	}

	cases := []struct {
		content  string
		expected map[string]bool // entity names we expect to find
	}{
		{
			content: "The handler is in /internal/api/handler.go and uses github.com/gin-gonic/gin",
			expected: map[string]bool{
				"/internal/api/handler.go": true,
				"github.com/gin-gonic/gin": true,
			},
		},
		{
			content: "See ./cmd/ycode/main.go for the entry point",
			expected: map[string]bool{
				"./cmd/ycode/main.go": true,
			},
		},
		{
			content: "Check https://docs.example.com/api for details",
			expected: map[string]bool{
				"https://docs.example.com/api": true,
			},
		},
		{
			content: "Uses github.com/stretchr/testify/assert for testing and /pkg/util/helper.go",
			expected: map[string]bool{
				"github.com/stretchr/testify/assert": true,
				"/pkg/util/helper.go":                true,
			},
		},
		{
			content:  "No entities in this plain text about memory management and retrieval quality.",
			expected: map[string]bool{},
		},
	}

	totalExpected := 0
	totalFound := 0
	truePositives := 0

	for _, tc := range cases {
		entities := memory.ExtractEntities(tc.content)
		foundNames := make(map[string]bool, len(entities))
		for _, e := range entities {
			foundNames[e.Name] = true
		}

		for name := range tc.expected {
			totalExpected++
			if foundNames[name] {
				truePositives++
			} else {
				t.Logf("  missed: %q in %q", name, tc.content)
			}
		}
		totalFound += len(entities)
	}

	precision := 0.0
	if totalFound > 0 {
		precision = float64(truePositives) / float64(totalFound)
	}
	recall := 0.0
	if totalExpected > 0 {
		recall = float64(truePositives) / float64(totalExpected)
	}

	t.Logf("Entity extraction: precision=%.4f recall=%.4f (TP=%d found=%d expected=%d)",
		precision, recall, truePositives, totalFound, totalExpected)

	if precision < 0.70 {
		t.Errorf("entity precision = %.4f, want >= 0.70", precision)
	}
	if recall < 0.60 {
		t.Errorf("entity recall = %.4f, want >= 0.60", recall)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Fusion Improves Over Single Backend
// ---------------------------------------------------------------------------

func TestMemory_FusionImprovesOverSingleBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fusion comparison in short mode")
	}

	memories, queries := buildContractDataset()

	// Baseline: keyword-only (no entity index).
	dir1 := t.TempDir()
	mgr1, _ := memory.NewManager(dir1)
	for _, mem := range memories {
		cp := *mem
		cp.FilePath = filepath.Join(dir1, cp.Name+".md")
		mgr1.Save(&cp)
	}

	// With entity index.
	dir2 := t.TempDir()
	mgr2, _ := memory.NewManager(dir2)
	ei := memory.NewEntityIndex()
	mgr2.SetEntityIndex(ei)
	for _, mem := range memories {
		cp := *mem
		cp.FilePath = filepath.Join(dir2, cp.Name+".md")
		mgr2.Save(&cp)
	}

	var baseNDCG, fusedNDCG float64
	for _, q := range queries {
		r1, _ := mgr1.Recall(q.Query, 5)
		r2, _ := mgr2.Recall(q.Query, 5)
		_, _, n1, _ := computeContractMetrics(r1, q.Relevant, 5)
		_, _, n2, _ := computeContractMetrics(r2, q.Relevant, 5)
		baseNDCG += n1
		fusedNDCG += n2
	}
	count := float64(len(queries))
	baseNDCG /= count
	fusedNDCG /= count

	t.Logf("Fusion comparison: keyword-only NDCG=%.4f, with-entity NDCG=%.4f", baseNDCG, fusedNDCG)

	// Fusion must not degrade results.
	if fusedNDCG < baseNDCG*0.95 {
		t.Errorf("entity fusion degraded NDCG: %.4f -> %.4f (>5%% drop)", baseNDCG, fusedNDCG)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Temporal Validity Ordering
// ---------------------------------------------------------------------------

func TestMemory_TemporalValidityOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping temporal validity ordering in short mode")
	}

	dir := t.TempDir()
	mgr, _ := memory.NewManager(dir)

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(24 * time.Hour)

	memories := []*memory.Memory{
		{Name: "expired-1", Description: "deploy target expired", Type: memory.TypeProject,
			Content: "Deploy to staging-1.", UpdatedAt: time.Now(), ValidUntil: &past},
		{Name: "expired-2", Description: "deploy config expired", Type: memory.TypeProject,
			Content: "Deploy config for staging-1.", UpdatedAt: time.Now(), ValidUntil: &past},
		{Name: "current-1", Description: "deploy target current", Type: memory.TypeProject,
			Content: "Deploy to staging-3.", UpdatedAt: time.Now()},
		{Name: "current-2", Description: "deploy config current", Type: memory.TypeProject,
			Content: "Deploy config for staging-3.", UpdatedAt: time.Now()},
		{Name: "future-1", Description: "deploy target future", Type: memory.TypeProject,
			Content: "Deploy to staging-4.", UpdatedAt: time.Now(), ValidFrom: &future},
	}

	for _, mem := range memories {
		mem.FilePath = filepath.Join(dir, mem.Name+".md")
		mgr.Save(mem)
	}

	results, _ := mgr.Recall("deploy target", 5)

	// Find positions.
	positions := make(map[string]int)
	for i, r := range results {
		positions[r.Memory.Name] = i
	}

	// Current memories should rank above expired ones.
	for _, cur := range []string{"current-1", "current-2"} {
		for _, exp := range []string{"expired-1", "expired-2"} {
			curPos, curOK := positions[cur]
			expPos, expOK := positions[exp]
			if curOK && expOK && curPos > expPos {
				t.Errorf("current %q (pos %d) should rank above expired %q (pos %d)",
					cur, curPos, exp, expPos)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5: Value Scoring Monotonicity
// ---------------------------------------------------------------------------

func TestMemory_ValueScoringMonotonicity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping value monotonicity in short mode")
	}

	dir := t.TempDir()
	mgr, _ := memory.NewManager(dir)

	// Create 10 memories with value scores from 0.1 to 1.0.
	for i := 1; i <= 10; i++ {
		mem := &memory.Memory{
			Name:        fmt.Sprintf("item-%02d", i),
			Description: "deployment configuration details",
			Type:        memory.TypeProject,
			Content:     "Deploy with rolling updates to the production cluster.",
			ValueScore:  float64(i) * 0.1,
			UpdatedAt:   time.Now(),
		}
		mem.FilePath = filepath.Join(dir, mem.Name+".md")
		mgr.Save(mem)
	}

	results, _ := mgr.Recall("deployment configuration", 10)

	// Count ordering violations in top-5.
	swaps := 0
	for i := 0; i+1 < len(results) && i < 4; i++ {
		if results[i].Memory.ValueScore < results[i+1].Memory.ValueScore {
			swaps++
		}
	}

	t.Logf("Value monotonicity: %d swaps in top-%d", swaps, min(5, len(results)))
	if swaps > 1 {
		t.Errorf("too many value ordering violations: %d swaps (want <= 1)", swaps)
		for i, r := range results {
			if i >= 5 {
				break
			}
			t.Logf("  %d: %s value=%.2f score=%.4f", i, r.Memory.Name, r.Memory.ValueScore, r.Score)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: Consolidation Preserves Content
// Tests via public Dreamer.Start() with immediate cancellation.
// ---------------------------------------------------------------------------

func TestMemory_ConsolidationPreservesContent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping consolidation content test in short mode")
	}

	dir := t.TempDir()
	mgr, _ := memory.NewManager(dir)

	keywords := []string{"OAuth2 PKCE", "Redis cache", "rolling updates"}
	for i, kw := range keywords {
		mgr.Save(&memory.Memory{
			Name:        fmt.Sprintf("deploy-dup-%d", i),
			Description: "deployment configuration for production environment",
			Type:        memory.TypeProject,
			Content:     fmt.Sprintf("Deploy instructions: %s. Target staging-3.", kw),
			FilePath:    filepath.Join(dir, fmt.Sprintf("deploy-dup-%d.md", i)),
		})
	}

	before, _ := mgr.All()
	if len(before) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(before))
	}

	// Run consolidation (heuristic, no LLM).
	d := memory.NewDreamer(mgr, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go d.Start(ctx) //nolint:errcheck
	// Wait for first consolidation pass.
	time.Sleep(500 * time.Millisecond)
	cancel()

	after, _ := mgr.All()
	t.Logf("Consolidation: %d -> %d memories", len(before), len(after))

	if len(after) >= len(before) {
		t.Errorf("consolidation should reduce count: %d -> %d", len(before), len(after))
	}

	// Check that the surviving memory contains content from at least one original.
	if len(after) > 0 {
		content := after[0].Content + " " + after[0].Description
		found := 0
		for _, kw := range []string{"staging-3", "deploy", "production"} {
			if strings.Contains(strings.ToLower(content), strings.ToLower(kw)) {
				found++
			}
		}
		if found == 0 {
			t.Errorf("surviving memory lost all key terms; content: %q", content)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 7: Turn Injection Budget Respected
// ---------------------------------------------------------------------------

func TestMemory_TurnInjectionBudgetRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping turn injection budget test in short mode")
	}

	dir := t.TempDir()
	mgr, _ := memory.NewManager(dir)

	// Create enough memories to potentially exceed budget.
	for i := range 10 {
		mgr.Save(&memory.Memory{
			Name:        fmt.Sprintf("info-%d", i),
			Description: fmt.Sprintf("topic %d about authentication and deployment configuration", i),
			Type:        memory.TypeProject,
			Content:     fmt.Sprintf("Detailed information about topic %d with lots of content that should fill up the budget. This includes authentication, deployment, and testing details for the production system.", i),
			FilePath:    filepath.Join(dir, fmt.Sprintf("info-%d.md", i)),
			UpdatedAt:   time.Now(),
		})
	}

	for _, budget := range []int{500, 1500} {
		ti := memory.NewTurnInjector(mgr, budget)
		result := ti.InjectForTurn(context.Background(), "authentication deployment configuration")

		// The budget applies to the content between tags, but the tags themselves
		// add overhead. Check that the total is reasonable (budget + tag overhead).
		maxLen := budget + 100 // allow for <memory-context> tags
		if len(result) > maxLen {
			t.Errorf("budget=%d: output length %d exceeds max %d", budget, len(result), maxLen)
		}
		t.Logf("budget=%d: output length=%d", budget, len(result))
	}
}

// ---------------------------------------------------------------------------
// Test 8: Turn Injection Deduplication
// ---------------------------------------------------------------------------

func TestMemory_TurnInjectionDeduplication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping turn injection dedup test in short mode")
	}

	dir := t.TempDir()
	mgr, _ := memory.NewManager(dir)

	mgr.Save(&memory.Memory{
		Name:        "auth-info",
		Description: "authentication configuration",
		Type:        memory.TypeProject,
		Content:     "Use OAuth2 with Auth0.",
		FilePath:    filepath.Join(dir, "auth-info.md"),
		UpdatedAt:   time.Now(),
	})

	ti := memory.NewTurnInjector(mgr, 1500)

	first := ti.InjectForTurn(context.Background(), "how does authentication work?")
	if first == "" {
		t.Fatal("first injection should return content")
	}

	// Same query again — should be deduped.
	second := ti.InjectForTurn(context.Background(), "how does authentication work?")
	if second != "" {
		t.Errorf("duplicate query should return empty, got %d chars", len(second))
	}

	// Sufficiently different query should return content.
	third := ti.InjectForTurn(context.Background(), "how to deploy to production?")
	// This may or may not return content depending on matching, so don't assert non-empty.
	_ = third
}

// ---------------------------------------------------------------------------
// Test 9: Cross-Scope Recall
// ---------------------------------------------------------------------------

func TestMemory_CrossScopeRecall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cross-scope recall in short mode")
	}

	globalDir := t.TempDir()
	projectDir := t.TempDir()

	mgr, err := memory.NewManagerWithGlobal(globalDir, projectDir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Save to project scope (default).
	mgr.Save(&memory.Memory{
		Name:        "project-deploy",
		Description: "project-specific deployment configuration",
		Type:        memory.TypeProject,
		Content:     "Deploy this project to staging-3.",
		FilePath:    filepath.Join(projectDir, "project-deploy.md"),
		UpdatedAt:   time.Now(),
	})

	// Save to global scope directly via the global store.
	globalStore := mgr.GlobalStore()
	if globalStore == nil {
		t.Fatal("global store should not be nil")
	}
	globalMem := &memory.Memory{
		Name:        "global-deploy",
		Description: "global deployment standards",
		Type:        memory.TypeProject,
		Content:     "All deployments use rolling updates with health checks.",
		FilePath:    filepath.Join(globalDir, "global-deploy.md"),
		UpdatedAt:   time.Now(),
	}
	globalStore.Save(globalMem)

	results, err := mgr.Recall("deployment configuration", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	foundProject := false
	foundGlobal := false
	var projectScore, globalScore float64
	for _, r := range results {
		switch r.Memory.Name {
		case "project-deploy":
			foundProject = true
			projectScore = r.Score
		case "global-deploy":
			foundGlobal = true
			globalScore = r.Score
		}
	}

	if !foundProject {
		t.Error("project-scoped memory not found in recall results")
	}
	if !foundGlobal {
		t.Error("global-scoped memory not found in recall results")
	}

	// Project-scoped should score higher due to 1.1x boost.
	if foundProject && foundGlobal && projectScore < globalScore {
		t.Logf("project score=%.4f global score=%.4f", projectScore, globalScore)
		t.Error("project-scoped memory should score >= global (1.1x boost)")
	}
}

// ---------------------------------------------------------------------------
// Test 10: Reward Propagation Convergence
// ---------------------------------------------------------------------------

func TestMemory_RewardConvergence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping reward convergence in short mode")
	}

	// Positive convergence: 0.5 → high.
	memPos := &memory.Memory{
		Name:       "pos",
		Importance: 0.5,
		UpdatedAt:  time.Now(),
	}
	for range 20 {
		memory.PropagateReward(memPos, 1.0, memory.DefaultRewardAlpha)
	}
	if memPos.ValueScore < 0.95 {
		t.Errorf("positive convergence: ValueScore = %.4f, want >= 0.95", memPos.ValueScore)
	}

	// Negative convergence: 0.5 → low.
	memNeg := &memory.Memory{
		Name:       "neg",
		Importance: 0.5,
		UpdatedAt:  time.Now(),
	}
	for range 20 {
		memory.PropagateReward(memNeg, 0.0, memory.DefaultRewardAlpha)
	}
	if memNeg.ValueScore > 0.05 {
		t.Errorf("negative convergence: ValueScore = %.4f, want <= 0.05", memNeg.ValueScore)
	}

	t.Logf("Reward convergence: positive=%.6f negative=%.6f", memPos.ValueScore, memNeg.ValueScore)
}
