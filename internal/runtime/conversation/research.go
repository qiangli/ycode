package conversation

import (
	"fmt"
	"strings"
)

// ResearchTask represents a sub-task in an auto-research workflow.
type ResearchTask struct {
	ID        string `json:"id"`
	Query     string `json:"query"`
	Status    string `json:"status"` // pending, in_progress, completed, failed
	Result    string `json:"result,omitempty"`
	AgentType string `json:"agent_type"`
}

// ResearchPlan is a structured breakdown of a research request.
type ResearchPlan struct {
	OriginalQuery string          `json:"original_query"`
	Tasks         []*ResearchTask `json:"tasks"`
	Summary       string          `json:"summary,omitempty"`
}

// NewResearchPlan creates a plan by decomposing a query into sub-tasks.
func NewResearchPlan(query string) *ResearchPlan {
	plan := &ResearchPlan{
		OriginalQuery: query,
	}

	// Decompose query into sub-tasks based on structure.
	subtasks := decomposeQuery(query)
	for i, q := range subtasks {
		plan.Tasks = append(plan.Tasks, &ResearchTask{
			ID:        fmt.Sprintf("research_%d", i+1),
			Query:     q,
			Status:    "pending",
			AgentType: classifyResearchTask(q),
		})
	}

	return plan
}

// NextPending returns the next pending task, or nil if all done.
func (rp *ResearchPlan) NextPending() *ResearchTask {
	for _, t := range rp.Tasks {
		if t.Status == "pending" {
			return t
		}
	}
	return nil
}

// IsComplete returns true if all tasks are completed or failed.
func (rp *ResearchPlan) IsComplete() bool {
	for _, t := range rp.Tasks {
		if t.Status == "pending" || t.Status == "in_progress" {
			return false
		}
	}
	return true
}

// CompletedResults returns results from all completed tasks.
func (rp *ResearchPlan) CompletedResults() []string {
	var results []string
	for _, t := range rp.Tasks {
		if t.Status == "completed" && t.Result != "" {
			results = append(results, t.Result)
		}
	}
	return results
}

// decomposeQuery splits a complex query into simpler sub-queries.
func decomposeQuery(query string) []string {
	// Split on common delimiters that indicate multiple questions.
	// Check for numbered lists.
	lines := strings.Split(query, "\n")
	var tasks []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Detect numbered items or bullet points.
		if len(line) > 2 && (line[0] == '-' || line[0] == '*' ||
			(line[0] >= '1' && line[0] <= '9' && (line[1] == '.' || line[1] == ')'))) {
			cleaned := strings.TrimLeft(line, "-*0123456789.) ")
			if cleaned != "" {
				tasks = append(tasks, cleaned)
			}
			continue
		}
	}

	// If no structured decomposition found, check for "and" conjunctions.
	if len(tasks) == 0 {
		// Split on " and " or "; " if query is long enough.
		if len(query) > 100 {
			parts := strings.Split(query, " and ")
			if len(parts) > 1 && len(parts) <= 5 {
				return parts
			}
		}
		// Return as single task.
		return []string{query}
	}

	return tasks
}

// classifyResearchTask determines the best agent type for a research sub-task.
func classifyResearchTask(query string) string {
	q := strings.ToLower(query)

	switch {
	case strings.Contains(q, "code") || strings.Contains(q, "function") ||
		strings.Contains(q, "file") || strings.Contains(q, "implement"):
		return "Explore"
	case strings.Contains(q, "architecture") || strings.Contains(q, "design") ||
		strings.Contains(q, "plan"):
		return "Plan"
	case strings.Contains(q, "search") || strings.Contains(q, "find") ||
		strings.Contains(q, "web") || strings.Contains(q, "url"):
		return "general-purpose"
	default:
		return "Explore"
	}
}
