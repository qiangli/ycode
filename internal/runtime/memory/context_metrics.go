package memory

import (
	"context"
	"strings"
	"unicode"
)

// CompactionFidelity measures how well a consolidated memory preserves
// the information from the original memories it replaced.
type CompactionFidelity struct {
	// KeywordCoverage is the fraction of unique keywords from the originals
	// that appear in the compacted memory's content + description.
	KeywordCoverage float64

	// LengthRatio is compacted_length / sum(original_lengths).
	// Values < 1.0 indicate actual compression occurred.
	LengthRatio float64
}

// MeasureCompactionFidelity scores how well a compacted memory preserves
// the content of the original memories it replaced.
func MeasureCompactionFidelity(originals []*Memory, compacted *Memory) CompactionFidelity {
	if len(originals) == 0 || compacted == nil {
		return CompactionFidelity{}
	}

	// Collect unique keywords from all originals (lowercased, >= 3 chars).
	originalKeywords := make(map[string]bool)
	totalOrigLen := 0
	for _, m := range originals {
		text := m.Content + " " + m.Description
		totalOrigLen += len(text)
		for _, word := range extractKeywords(text) {
			originalKeywords[word] = true
		}
	}

	// Check how many appear in the compacted memory.
	compactedText := strings.ToLower(compacted.Content + " " + compacted.Description)
	found := 0
	for kw := range originalKeywords {
		if strings.Contains(compactedText, kw) {
			found++
		}
	}

	coverage := 0.0
	if len(originalKeywords) > 0 {
		coverage = float64(found) / float64(len(originalKeywords))
	}

	ratio := 0.0
	if totalOrigLen > 0 {
		ratio = float64(len(compacted.Content)+len(compacted.Description)) / float64(totalOrigLen)
	}

	return CompactionFidelity{
		KeywordCoverage: coverage,
		LengthRatio:     ratio,
	}
}

// InjectionMetrics tracks quality of per-turn memory injection.
type InjectionMetrics struct {
	// RelevanceRate is the fraction of injected memories that were actually relevant.
	RelevanceRate float64

	// BudgetUtilization is chars_used / budget (how well the budget is used).
	BudgetUtilization float64
}

// TurnScenario defines a single turn for injection testing with ground truth.
type TurnScenario struct {
	Query            string
	RelevantMemories []string // memory names that should be injected
}

// MeasureInjection evaluates turn injection quality over a sequence of turns.
func MeasureInjection(ti *TurnInjector, turns []TurnScenario) InjectionMetrics {
	if len(turns) == 0 {
		return InjectionMetrics{}
	}

	totalRelevant := 0
	totalInjected := 0
	totalChars := 0

	for _, turn := range turns {
		result := ti.InjectForTurn(context.TODO(), turn.Query)
		if result == "" {
			continue
		}

		totalChars += len(result)

		// Count how many relevant memories appear in the injected text.
		relevantSet := make(map[string]bool, len(turn.RelevantMemories))
		for _, name := range turn.RelevantMemories {
			relevantSet[name] = true
		}

		// Check each relevant memory name in the output.
		for name := range relevantSet {
			totalInjected++
			if strings.Contains(result, name) {
				totalRelevant++
			}
		}
	}

	relevanceRate := 0.0
	if totalInjected > 0 {
		relevanceRate = float64(totalRelevant) / float64(totalInjected)
	}

	budgetUtil := 0.0
	if ti.budget > 0 && len(turns) > 0 {
		budgetUtil = float64(totalChars) / float64(ti.budget*len(turns))
	}

	return InjectionMetrics{
		RelevanceRate:     relevanceRate,
		BudgetUtilization: budgetUtil,
	}
}

// ConsolidationMetrics tracks the effectiveness of memory consolidation.
type ConsolidationMetrics struct {
	CountBefore     int
	CountAfter      int
	CompactionRatio float64 // after / before (lower = more compaction)
	QualityDelta    float64 // retrieval quality change (negative = degradation)
}

// extractKeywords returns lowercased words of 3+ characters from text,
// filtering out common stop words.
func extractKeywords(text string) []string {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "from": true,
		"that": true, "this": true, "are": true, "was": true, "were": true,
		"has": true, "have": true, "had": true, "not": true, "but": true,
		"all": true, "can": true, "use": true, "via": true, "per": true,
	}

	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		if len(w) >= 3 && !stopWords[w] && !seen[w] {
			seen[w] = true
			keywords = append(keywords, w)
		}
	}
	return keywords
}
