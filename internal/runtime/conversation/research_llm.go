package conversation

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ResearchPlanV2 extends the basic ResearchPlan with a dependency DAG
// and synthesis prompt for LLM-powered research decomposition.
type ResearchPlanV2 struct {
	OriginalQuery string              `json:"original_query"`
	Tasks         []*ResearchTask     `json:"tasks"`
	Dependencies  map[string][]string `json:"dependencies"` // task ID -> prerequisite IDs
	Synthesizer   string              `json:"synthesizer"`  // prompt for final synthesis
}

// NewResearchPlanV2 creates an empty v2 plan.
func NewResearchPlanV2(query string) *ResearchPlanV2 {
	return &ResearchPlanV2{
		OriginalQuery: query,
		Dependencies:  make(map[string][]string),
	}
}

// AddTask adds a task to the plan.
func (rp *ResearchPlanV2) AddTask(id, query, agentType string, deps []string) {
	rp.Tasks = append(rp.Tasks, &ResearchTask{
		ID:        id,
		Query:     query,
		Status:    "pending",
		AgentType: agentType,
	})
	if len(deps) > 0 {
		rp.Dependencies[id] = deps
	}
}

// IsComplete returns true if all tasks are completed or failed.
func (rp *ResearchPlanV2) IsComplete() bool {
	for _, t := range rp.Tasks {
		if t.Status == "pending" || t.Status == "in_progress" {
			return false
		}
	}
	return true
}

// CompletedResults returns results from all completed tasks.
func (rp *ResearchPlanV2) CompletedResults() []string {
	var results []string
	for _, t := range rp.Tasks {
		if t.Status == "completed" && t.Result != "" {
			results = append(results, t.Result)
		}
	}
	return results
}

// Ready returns tasks whose prerequisites are all completed.
func (rp *ResearchPlanV2) Ready() []*ResearchTask {
	completed := make(map[string]bool)
	for _, t := range rp.Tasks {
		if t.Status == "completed" {
			completed[t.ID] = true
		}
	}

	var ready []*ResearchTask
	for _, t := range rp.Tasks {
		if t.Status != "pending" {
			continue
		}
		deps := rp.Dependencies[t.ID]
		allMet := true
		for _, d := range deps {
			if !completed[d] {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, t)
		}
	}
	return ready
}

// LLMDecomposition represents the structured output from an LLM decomposition call.
type LLMDecomposition struct {
	SubQueries []struct {
		ID        string   `json:"id"`
		Query     string   `json:"query"`
		AgentType string   `json:"agent_type"`
		DependsOn []string `json:"depends_on,omitempty"`
	} `json:"sub_queries"`
	SynthesisPrompt string `json:"synthesis_prompt"`
}

// DecompositionPrompt returns the system prompt for LLM-based query decomposition.
func DecompositionPrompt() string {
	return `You are a research planner. Given a complex query, decompose it into smaller, independent sub-queries that can be researched in parallel where possible.

Output a JSON object with this structure:
{
  "sub_queries": [
    {"id": "q1", "query": "...", "agent_type": "Explore|Plan|general-purpose", "depends_on": []},
    {"id": "q2", "query": "...", "agent_type": "Explore", "depends_on": ["q1"]}
  ],
  "synthesis_prompt": "Combine the findings to answer: ..."
}

Rules:
- Use "Explore" for code/file investigation tasks
- Use "Plan" for architecture/design analysis tasks
- Use "general-purpose" for web search or broad research tasks
- Minimize dependencies to maximize parallelism
- Each sub-query should be self-contained and answerable independently
- Keep to 2-8 sub-queries`
}

// ParseDecomposition parses an LLM decomposition response into a ResearchPlanV2.
func ParseDecomposition(query, llmResponse string) (*ResearchPlanV2, error) {
	// Try to extract JSON from the response (may have surrounding text).
	jsonStr := extractJSON(llmResponse)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in decomposition response")
	}

	var decomp LLMDecomposition
	if err := json.Unmarshal([]byte(jsonStr), &decomp); err != nil {
		return nil, fmt.Errorf("parse decomposition: %w", err)
	}

	if len(decomp.SubQueries) == 0 {
		return nil, fmt.Errorf("decomposition produced no sub-queries")
	}

	plan := NewResearchPlanV2(query)
	plan.Synthesizer = decomp.SynthesisPrompt

	for _, sq := range decomp.SubQueries {
		plan.AddTask(sq.ID, sq.Query, sq.AgentType, sq.DependsOn)
	}

	return plan, nil
}

// extractJSON finds the first JSON object in a string (handles markdown code blocks).
func extractJSON(s string) string {
	// Try markdown code block first.
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + 3
		// Skip optional language tag.
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	// Try raw JSON.
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
	return ""
}
