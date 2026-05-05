package memory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// RecallFlowConfig configures adaptive-depth retrieval.
type RecallFlowConfig struct {
	ConfidenceHigh float64 // above this, return immediately (default 0.7)
	ConfidenceLow  float64 // below this, trigger deepening (default 0.3)
	MaxExploration int     // max deepening iterations (default 1)
	MinQueryLen    int     // skip LLM analysis below this char count (default 100)
}

// DefaultRecallFlowConfig returns sensible defaults.
func DefaultRecallFlowConfig() RecallFlowConfig {
	return RecallFlowConfig{
		ConfidenceHigh: 0.7,
		ConfidenceLow:  0.3,
		MaxExploration: 1,
		MinQueryLen:    100,
	}
}

// RecallFlow implements adaptive-depth retrieval inspired by CrewAI's RecallFlow.
// If initial results have low confidence, it iteratively deepens search with
// LLM-generated sub-queries and gap analysis.
type RecallFlow struct {
	manager *Manager
	llmFunc func(system, user string) (string, error)
	config  RecallFlowConfig
	logger  *slog.Logger
}

// NewRecallFlow creates an adaptive recall flow.
func NewRecallFlow(manager *Manager, llmFunc func(system, user string) (string, error), config RecallFlowConfig) *RecallFlow {
	if config.ConfidenceHigh <= 0 {
		config.ConfidenceHigh = 0.7
	}
	if config.ConfidenceLow <= 0 {
		config.ConfidenceLow = 0.3
	}
	if config.MaxExploration <= 0 {
		config.MaxExploration = 1
	}
	if config.MinQueryLen <= 0 {
		config.MinQueryLen = 100
	}
	return &RecallFlow{
		manager: manager,
		llmFunc: llmFunc,
		config:  config,
		logger:  slog.Default(),
	}
}

// Recall performs adaptive-depth retrieval.
func (rf *RecallFlow) Recall(query string, maxResults int) ([]SearchResult, error) {
	// Step 1: Query analysis (skip for short queries).
	subQueries := []string{query}
	if len(query) >= rf.config.MinQueryLen && rf.llmFunc != nil {
		analyzed := rf.analyzeQuery(query)
		if len(analyzed) > 0 {
			subQueries = analyzed
		}
	}

	// Step 2: Initial search across sub-queries.
	var allResults []SearchResult
	seen := make(map[string]bool)

	for _, sq := range subQueries {
		results, err := rf.manager.Recall(sq, maxResults)
		if err != nil {
			continue
		}
		for _, r := range results {
			if !seen[r.Memory.Name] {
				seen[r.Memory.Name] = true
				allResults = append(allResults, r)
			}
		}
	}

	if len(allResults) == 0 {
		return nil, nil
	}

	// Step 3: Confidence assessment.
	topScore := allResults[0].Score
	if topScore >= rf.config.ConfidenceHigh {
		rf.logger.Debug("recallflow: high confidence, returning", "score", topScore)
		return truncResults(allResults, maxResults), nil
	}

	// Step 4: Deepening (if low confidence and budget available).
	if topScore < rf.config.ConfidenceLow && rf.llmFunc != nil {
		for i := 0; i < rf.config.MaxExploration; i++ {
			refined := rf.identifyGaps(query, allResults)
			if len(refined) == 0 {
				break
			}

			for _, rq := range refined {
				results, err := rf.manager.Recall(rq, maxResults)
				if err != nil {
					continue
				}
				for _, r := range results {
					if !seen[r.Memory.Name] {
						seen[r.Memory.Name] = true
						allResults = append(allResults, r)
					}
				}
			}
		}
	}

	// Step 5: Sort by score and return.
	sortResults(allResults)
	return truncResults(allResults, maxResults), nil
}

// analyzeQuery decomposes a complex query into sub-queries using LLM.
func (rf *RecallFlow) analyzeQuery(query string) []string {
	system := `You are a query decomposition engine. Given a complex query, break it into 1-3 simpler sub-queries that together cover the full intent. Return JSON: {"queries": ["sub-query 1", "sub-query 2"]}`
	response, err := rf.llmFunc(system, query)
	if err != nil {
		return nil
	}

	var result struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromRecallResponse(response)), &result); err != nil {
		return nil
	}
	return result.Queries
}

// identifyGaps asks LLM what information is missing from current results.
func (rf *RecallFlow) identifyGaps(query string, results []SearchResult) []string {
	var resultSummary strings.Builder
	for i, r := range results {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&resultSummary, "- %s: %s\n", r.Memory.Name, r.Memory.Description)
	}

	system := `Given a query and partial results, identify what information is missing. Return JSON: {"queries": ["refined query 1"]}`
	user := fmt.Sprintf("Query: %s\n\nCurrent results:\n%s\nWhat is missing?", query, resultSummary.String())

	response, err := rf.llmFunc(system, user)
	if err != nil {
		return nil
	}

	var result struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromRecallResponse(response)), &result); err != nil {
		return nil
	}
	return result.Queries
}

// sortResults sorts by score descending.
func sortResults(results []SearchResult) {
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

func truncResults(results []SearchResult, max int) []SearchResult {
	if len(results) > max {
		return results[:max]
	}
	return results
}

// extractJSONFromRecallResponse extracts JSON from LLM response.
func extractJSONFromRecallResponse(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		return s
	}
	if idx := strings.Index(s, "{"); idx >= 0 {
		depth := 0
		for i := idx; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return s[idx : i+1]
				}
			}
		}
	}
	return "{}"
}
