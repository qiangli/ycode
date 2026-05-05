package memory

import (
	"sort"
	"strings"
)

// SearchResult pairs a memory with a relevance score.
type SearchResult struct {
	Memory *Memory
	Score  float64
	Source string // which search backend produced this result (e.g., "vector", "bleve", "keyword", "entity")
}

// Search finds memories relevant to a query.
func Search(memories []*Memory, query string, maxResults int) []SearchResult {
	if len(memories) == 0 {
		return nil
	}

	queryLower := strings.ToLower(query)
	queryParts := strings.Fields(queryLower)

	var results []SearchResult
	for _, mem := range memories {
		score := scoreMemory(mem, queryParts)
		if score > 0 {
			results = append(results, SearchResult{Memory: mem, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

// scoreMemory computes relevance of a memory to query parts.
func scoreMemory(mem *Memory, queryParts []string) float64 {
	score := 0.0
	nameLower := strings.ToLower(mem.Name)
	descLower := strings.ToLower(mem.Description)
	contentLower := strings.ToLower(mem.Content)

	for _, part := range queryParts {
		if strings.Contains(nameLower, part) {
			score += 3.0
		}
		if strings.Contains(descLower, part) {
			score += 2.0
		}
		if strings.Contains(contentLower, part) {
			score += 1.0
		}
	}

	return score
}
