package tools

import (
	"sort"
	"strings"
)

// ScoredSkill is a skill with a relevance score.
type ScoredSkill struct {
	Name        string
	Description string
	Path        string
	Score       float64
}

// RankSkills ranks skill candidates by relevance to a query.
// Uses keyword matching as the base scorer. When Bleve/vector indices
// are available, those scores are blended in by the caller.
func RankSkills(query string, candidates []ScoredSkill) []ScoredSkill {
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	for i := range candidates {
		score := 0.0
		nameLower := strings.ToLower(candidates[i].Name)
		descLower := strings.ToLower(candidates[i].Description)

		// Exact name match.
		if nameLower == queryLower {
			score += 10.0
		}
		// Name contains query.
		if strings.Contains(nameLower, queryLower) {
			score += 5.0
		}
		// Term matching in name and description.
		for _, term := range queryTerms {
			if strings.Contains(nameLower, term) {
				score += 2.0
			}
			if strings.Contains(descLower, term) {
				score += 1.0
			}
		}
		// Blend with any existing score (from external rankers).
		candidates[i].Score += score
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return candidates
}

// TopN returns the top N ranked skills.
func TopN(skills []ScoredSkill, n int) []ScoredSkill {
	if n >= len(skills) {
		return skills
	}
	return skills[:n]
}
