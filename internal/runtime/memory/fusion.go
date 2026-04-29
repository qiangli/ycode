package memory

import (
	"math"
	"sort"
	"strings"
)

// FusionWeights controls retrieval fusion parameters.
type FusionWeights struct {
	RRFk      float64 // Reciprocal Rank Fusion constant (default 60)
	MMRLambda float64 // MMR relevance vs diversity balance (default 0.7)
}

// DefaultFusionWeights returns default fusion parameters.
func DefaultFusionWeights() FusionWeights {
	return FusionWeights{RRFk: 60, MMRLambda: 0.7}
}

// ReciprocalRankFusion fuses ranked result lists from multiple backends
// using the RRF formula: score(d) = sum_over_backends(1 / (k + rank)).
// Each backend's results must be pre-sorted by descending score.
func ReciprocalRankFusion(resultSets map[string][]SearchResult, k float64) []SearchResult {
	if k <= 0 {
		k = 60
	}

	// Accumulate fused scores by memory name.
	type fusedEntry struct {
		memory *Memory
		score  float64
		source string // first backend that contributed this result
	}
	entries := make(map[string]*fusedEntry)

	for backend, results := range resultSets {
		for rank, r := range results {
			name := r.Memory.Name
			rrfScore := 1.0 / (k + float64(rank+1))
			if e, ok := entries[name]; ok {
				e.score += rrfScore
			} else {
				entries[name] = &fusedEntry{
					memory: r.Memory,
					score:  rrfScore,
					source: backend,
				}
			}
			_ = backend // used above
		}
	}

	// Flatten to sorted slice.
	out := make([]SearchResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, SearchResult{
			Memory: e.memory,
			Score:  e.score,
			Source: e.source,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})

	return out
}

// MMRRerank applies Maximal Marginal Relevance re-ranking to balance
// relevance and diversity. lambda controls the tradeoff: 1.0 = pure relevance,
// 0.0 = pure diversity.
func MMRRerank(results []SearchResult, lambda float64, maxResults int) []SearchResult {
	if len(results) <= 1 || maxResults <= 0 {
		return results
	}
	if maxResults > len(results) {
		maxResults = len(results)
	}

	selected := make([]SearchResult, 0, maxResults)
	remaining := make([]SearchResult, len(results))
	copy(remaining, results)

	// Select first result (highest fused score).
	selected = append(selected, remaining[0])
	remaining = remaining[1:]

	for len(selected) < maxResults && len(remaining) > 0 {
		bestIdx := -1
		bestMMR := math.Inf(-1)

		for i, candidate := range remaining {
			// Max similarity to any already-selected result.
			maxSim := 0.0
			for _, s := range selected {
				sim := jaccardSimilarity(memoryWords(candidate.Memory), memoryWords(s.Memory))
				if sim > maxSim {
					maxSim = sim
				}
			}

			mmrScore := lambda*candidate.Score - (1-lambda)*maxSim
			if mmrScore > bestMMR {
				bestMMR = mmrScore
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}

		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

// jaccardSimilarity computes Jaccard index between two word sets.
func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}

	intersection := 0
	for w := range a {
		if _, ok := b[w]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// memoryWords returns the lowercased word set of a memory's text fields.
func memoryWords(mem *Memory) map[string]struct{} {
	text := strings.ToLower(mem.Name + " " + mem.Description + " " + mem.Content)
	words := strings.Fields(text)
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[w] = struct{}{}
	}
	return set
}
