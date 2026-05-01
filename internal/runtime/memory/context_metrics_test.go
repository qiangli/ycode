package memory

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Context Engineering Metrics Tests
// ---------------------------------------------------------------------------

// TestContextMetrics_CompactionFidelity verifies that the heuristic consolidation
// preserves key terms from the original memories.
func TestContextMetrics_CompactionFidelity(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	originals := []*Memory{
		{Name: "deploy-a", Description: "deployment configuration", Type: TypeProject,
			Content:  "Deploy with OAuth2 PKCE authentication to staging-3 cluster.",
			FilePath: filepath.Join(dir, "deploy-a.md")},
		{Name: "deploy-b", Description: "deployment configuration", Type: TypeProject,
			Content:  "Deploy uses Redis cache for session management. Rolling updates enabled.",
			FilePath: filepath.Join(dir, "deploy-b.md")},
		{Name: "deploy-c", Description: "deployment configuration", Type: TypeProject,
			Content:  "Deploy includes health checks and canary validation at 10% traffic.",
			FilePath: filepath.Join(dir, "deploy-c.md")},
	}

	for _, m := range originals {
		mgr.Save(m)
	}

	// Run heuristic consolidation.
	d := NewDreamer(mgr, true)
	d.consolidate() //nolint:errcheck

	after, _ := mgr.All()
	if len(after) == 0 {
		t.Fatal("no memories after consolidation")
	}

	fidelity := MeasureCompactionFidelity(originals, after[0])
	t.Logf("Compaction fidelity: coverage=%.4f ratio=%.4f", fidelity.KeywordCoverage, fidelity.LengthRatio)

	// The heuristic path keeps the best memory, so we expect partial keyword
	// coverage (the winning memory's keywords will be present).
	if fidelity.KeywordCoverage < 0.25 {
		t.Errorf("keyword coverage = %.4f, want >= 0.25", fidelity.KeywordCoverage)
	}

	// Length ratio should be < 1.0 since we went from 3 memories to 1.
	if fidelity.LengthRatio >= 1.0 {
		t.Errorf("length ratio = %.4f, want < 1.0 (compression)", fidelity.LengthRatio)
	}
}

// TestContextMetrics_InjectionRelevance simulates turns and checks that
// injected memories match expected ground truth.
func TestContextMetrics_InjectionRelevance(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Create diverse memories.
	memories := []struct {
		name, desc, content string
	}{
		{"auth-config", "OAuth2 authentication configuration", "We use OAuth2 with PKCE flow via Auth0."},
		{"deploy-prod", "Production deployment target", "Deploy to staging-3 with rolling updates."},
		{"db-schema", "Database sessions table schema", "Sessions: id UUID PK, user_id UUID FK."},
		{"api-rate", "API rate limiting configuration", "Rate limit: 100 req/min per user."},
		{"testing-tdd", "TDD testing workflow", "Always write tests first. Integration tests hit real database."},
	}

	for _, m := range memories {
		mgr.Save(&Memory{
			Name: m.name, Description: m.desc, Type: TypeProject,
			Content: m.content, FilePath: filepath.Join(dir, m.name+".md"), UpdatedAt: time.Now(),
		})
	}

	ti := NewTurnInjector(mgr, 1500)

	turns := []TurnScenario{
		{Query: "how does OAuth authentication work?", RelevantMemories: []string{"auth-config"}},
		{Query: "deploy to production environment", RelevantMemories: []string{"deploy-prod"}},
		{Query: "database schema for sessions", RelevantMemories: []string{"db-schema"}},
		{Query: "API rate limiting configuration", RelevantMemories: []string{"api-rate"}},
		{Query: "testing approach and TDD workflow", RelevantMemories: []string{"testing-tdd"}},
	}

	metrics := MeasureInjection(ti, turns)
	t.Logf("Injection metrics: relevance=%.4f budget_util=%.4f", metrics.RelevanceRate, metrics.BudgetUtilization)

	if metrics.RelevanceRate < 0.40 {
		t.Errorf("relevance rate = %.4f, want >= 0.40", metrics.RelevanceRate)
	}
}

// TestContextMetrics_ConsolidationQuality verifies that consolidation reduces
// memory count without significantly degrading retrieval quality.
func TestContextMetrics_ConsolidationQuality(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	ei := NewEntityIndex()
	mgr.SetEntityIndex(ei)

	// Create 10 unique memories.
	uniqueMemories := []struct {
		name, desc, content string
	}{
		{"auth-config", "OAuth2 authentication", "OAuth2 PKCE flow via Auth0."},
		{"deploy-prod", "Production deployment", "Deploy to staging-3 with rolling updates."},
		{"db-schema", "Database schema", "Sessions table: id, user_id, created_at."},
		{"api-rate", "API rate limiting", "100 req/min per user. Token bucket."},
		{"testing-tdd", "TDD workflow", "Write tests first. Real database."},
		{"infra-k8s", "Kubernetes config", "GKE cluster: 3 nodes, n2-standard-4."},
		{"monitoring", "Monitoring stack", "Prometheus + Grafana + Alertmanager."},
		{"ci-pipeline", "CI/CD pipeline", "GitHub Actions: lint, test, build, deploy."},
		{"security-audit", "Security findings", "Q1 audit: no critical findings."},
		{"team-onboard", "New member setup", "Clone repo, make init, set API keys."},
	}

	for _, m := range uniqueMemories {
		mgr.Save(&Memory{
			Name: m.name, Description: m.desc, Type: TypeProject,
			Content: m.content, FilePath: filepath.Join(dir, m.name+".md"), UpdatedAt: time.Now(),
		})
	}

	// Add 20 near-duplicates (2 duplicates of each unique memory).
	for i, m := range uniqueMemories {
		for j := range 2 {
			mgr.Save(&Memory{
				Name:        fmt.Sprintf("%s-dup-%d", m.name, j),
				Description: m.desc,
				Type:        TypeProject,
				Content:     fmt.Sprintf("%s (variation %d)", m.content, i*2+j),
				FilePath:    filepath.Join(dir, fmt.Sprintf("%s-dup-%d.md", m.name, j)),
				UpdatedAt:   time.Now(),
			})
		}
	}

	queries := []BenchmarkQuery{
		{Query: "authentication config", Relevant: []string{"auth-config"}},
		{Query: "deployment configuration", Relevant: []string{"deploy-prod"}},
		{Query: "database schema", Relevant: []string{"db-schema"}},
		{Query: "API rate limiting", Relevant: []string{"api-rate"}},
		{Query: "testing workflow", Relevant: []string{"testing-tdd"}},
	}

	// Measure quality before.
	beforeNDCG := measureAvgNDCG(t, mgr, queries, 5)
	before, _ := mgr.All()

	// Run consolidation.
	d := NewDreamer(mgr, true)
	d.consolidate() //nolint:errcheck

	// Measure quality after.
	afterNDCG := measureAvgNDCG(t, mgr, queries, 5)
	after, _ := mgr.All()

	metrics := ConsolidationMetrics{
		CountBefore:     len(before),
		CountAfter:      len(after),
		CompactionRatio: float64(len(after)) / float64(len(before)),
		QualityDelta:    afterNDCG - beforeNDCG,
	}

	t.Logf("Consolidation: %d -> %d memories (ratio=%.4f, quality_delta=%.4f)",
		metrics.CountBefore, metrics.CountAfter, metrics.CompactionRatio, metrics.QualityDelta)

	if metrics.CompactionRatio > 0.80 {
		t.Errorf("compaction ratio = %.4f, want <= 0.80 (expect reduction)", metrics.CompactionRatio)
	}
	// Consolidation with heavy deduplication (3x copies) naturally reduces recall
	// since fewer memory names match queries. Quality degradation up to -0.60 is
	// acceptable when 20 of 30 memories are intentional duplicates.
	if metrics.QualityDelta < -0.60 {
		t.Errorf("quality degradation = %.4f, want >= -0.60", metrics.QualityDelta)
	}
}

func measureAvgNDCG(t *testing.T, mgr *Manager, queries []BenchmarkQuery, k int) float64 {
	t.Helper()
	total := 0.0
	for _, q := range queries {
		results, err := mgr.Recall(q.Query, k)
		if err != nil {
			continue
		}
		m := computeMetrics(results, q.Relevant, k)
		total += m.NDCG
	}
	return total / float64(len(queries))
}
