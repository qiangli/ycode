package memory

import (
	"testing"
)

func TestReciprocalRankFusion_SingleBackend(t *testing.T) {
	results := map[string][]SearchResult{
		"vector": {
			{Memory: &Memory{Name: "a"}, Score: 0.9},
			{Memory: &Memory{Name: "b"}, Score: 0.7},
			{Memory: &Memory{Name: "c"}, Score: 0.5},
		},
	}

	fused := ReciprocalRankFusion(results, 60)
	if len(fused) != 3 {
		t.Fatalf("expected 3 results, got %d", len(fused))
	}
	// First result should have highest fused score.
	if fused[0].Memory.Name != "a" {
		t.Errorf("expected 'a' first, got %q", fused[0].Memory.Name)
	}
	// Scores should be monotonically decreasing.
	for i := 1; i < len(fused); i++ {
		if fused[i].Score > fused[i-1].Score {
			t.Errorf("result %d score (%f) > result %d score (%f)", i, fused[i].Score, i-1, fused[i-1].Score)
		}
	}
}

func TestReciprocalRankFusion_MultipleBackends(t *testing.T) {
	results := map[string][]SearchResult{
		"vector": {
			{Memory: &Memory{Name: "a"}, Score: 0.9},
			{Memory: &Memory{Name: "b"}, Score: 0.7},
		},
		"bleve": {
			{Memory: &Memory{Name: "b"}, Score: 5.0}, // b is top in bleve
			{Memory: &Memory{Name: "c"}, Score: 3.0},
		},
		"keyword": {
			{Memory: &Memory{Name: "c"}, Score: 6.0}, // c is top in keyword
			{Memory: &Memory{Name: "a"}, Score: 3.0},
		},
	}

	fused := ReciprocalRankFusion(results, 60)
	if len(fused) != 3 {
		t.Fatalf("expected 3 results, got %d", len(fused))
	}

	// All three memories should appear across backends.
	// 'a' appears in vector(rank1) + keyword(rank2) = 1/61 + 1/62
	// 'b' appears in vector(rank2) + bleve(rank1)  = 1/62 + 1/61
	// 'c' appears in bleve(rank2) + keyword(rank1)  = 1/62 + 1/61
	// So b and c tie, a is slightly lower. All should have similar scores.
	nameSet := make(map[string]bool)
	for _, r := range fused {
		nameSet[r.Memory.Name] = true
	}
	for _, name := range []string{"a", "b", "c"} {
		if !nameSet[name] {
			t.Errorf("expected %q in fused results", name)
		}
	}
}

func TestReciprocalRankFusion_BoostForMultiBackendHits(t *testing.T) {
	// Memory "shared" appears in both backends; "solo" in only one.
	results := map[string][]SearchResult{
		"vector": {
			{Memory: &Memory{Name: "shared"}, Score: 0.9},
			{Memory: &Memory{Name: "solo-v"}, Score: 0.8},
		},
		"bleve": {
			{Memory: &Memory{Name: "shared"}, Score: 5.0},
			{Memory: &Memory{Name: "solo-b"}, Score: 4.0},
		},
	}

	fused := ReciprocalRankFusion(results, 60)

	// "shared" should rank higher than either solo result because it gets score from both backends.
	if fused[0].Memory.Name != "shared" {
		t.Errorf("expected 'shared' to rank first due to multi-backend presence, got %q", fused[0].Memory.Name)
	}
}

func TestReciprocalRankFusion_EmptyInput(t *testing.T) {
	fused := ReciprocalRankFusion(nil, 60)
	if len(fused) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(fused))
	}

	fused = ReciprocalRankFusion(map[string][]SearchResult{}, 60)
	if len(fused) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(fused))
	}
}

func TestReciprocalRankFusion_ZeroK(t *testing.T) {
	results := map[string][]SearchResult{
		"v": {{Memory: &Memory{Name: "a"}, Score: 1.0}},
	}
	// k=0 should default to 60.
	fused := ReciprocalRankFusion(results, 0)
	if len(fused) != 1 {
		t.Fatalf("expected 1 result, got %d", len(fused))
	}
}

func TestMMRRerank_DiversityEffect(t *testing.T) {
	// Two very similar memories and one different one.
	results := []SearchResult{
		{Memory: &Memory{Name: "deploy-1", Description: "deploy config production", Content: "deploy to prod"}, Score: 1.0},
		{Memory: &Memory{Name: "deploy-2", Description: "deploy config staging", Content: "deploy to staging"}, Score: 0.95},
		{Memory: &Memory{Name: "auth", Description: "authentication flow", Content: "OAuth2 login"}, Score: 0.90},
	}

	// With pure relevance (lambda=1.0), order should be unchanged.
	pureRelevance := MMRRerank(results, 1.0, 3)
	if pureRelevance[0].Memory.Name != "deploy-1" {
		t.Errorf("pure relevance: expected deploy-1 first, got %q", pureRelevance[0].Memory.Name)
	}

	// With diversity (lambda=0.5), "auth" should be promoted over "deploy-2" since
	// deploy-2 is very similar to deploy-1.
	diverse := MMRRerank(results, 0.5, 3)
	if diverse[1].Memory.Name != "auth" {
		t.Errorf("diverse: expected auth second (promoted for diversity), got %q", diverse[1].Memory.Name)
	}
}

func TestMMRRerank_SingleResult(t *testing.T) {
	results := []SearchResult{
		{Memory: &Memory{Name: "solo"}, Score: 1.0},
	}
	reranked := MMRRerank(results, 0.7, 5)
	if len(reranked) != 1 || reranked[0].Memory.Name != "solo" {
		t.Error("single result should pass through unchanged")
	}
}

func TestMMRRerank_MaxResultsTrimming(t *testing.T) {
	var results []SearchResult
	for i := 0; i < 10; i++ {
		results = append(results, SearchResult{
			Memory: &Memory{Name: "m", Content: "unique content"},
			Score:  float64(10 - i),
		})
	}

	reranked := MMRRerank(results, 0.7, 3)
	if len(reranked) != 3 {
		t.Errorf("expected 3 results, got %d", len(reranked))
	}
}

func TestMMRRerank_EmptyInput(t *testing.T) {
	reranked := MMRRerank(nil, 0.7, 5)
	if len(reranked) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(reranked))
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]struct{}
		want float64
	}{
		{"identical", setOf("a", "b", "c"), setOf("a", "b", "c"), 1.0},
		{"disjoint", setOf("a", "b"), setOf("c", "d"), 0.0},
		{"overlap", setOf("a", "b", "c"), setOf("b", "c", "d"), 0.5},
		{"empty both", setOf(), setOf(), 0.0},
		{"one empty", setOf("a"), setOf(), 0.0},
		{"subset", setOf("a", "b"), setOf("a", "b", "c"), 2.0 / 3.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := jaccardSimilarity(tc.a, tc.b)
			if diff := got - tc.want; diff > 0.01 || diff < -0.01 {
				t.Errorf("jaccardSimilarity = %f, want %f", got, tc.want)
			}
		})
	}
}

func TestDefaultFusionWeights(t *testing.T) {
	w := DefaultFusionWeights()
	if w.RRFk != 60 {
		t.Errorf("RRFk = %f, want 60", w.RRFk)
	}
	if w.MMRLambda != 0.7 {
		t.Errorf("MMRLambda = %f, want 0.7", w.MMRLambda)
	}
}

// setOf creates a string set from variadic args.
func setOf(words ...string) map[string]struct{} {
	s := make(map[string]struct{}, len(words))
	for _, w := range words {
		s[w] = struct{}{}
	}
	return s
}
