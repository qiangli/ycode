package memory

import (
	"fmt"
	"strings"
)

// ConsolidationDecision represents the LLM's decision about similar memories.
type ConsolidationDecision struct {
	Action string // "merge", "keep_best", "delete_redundant"
	Result string // merged content (for merge action)
}

// FormatConsolidationPrompt takes a group of similar memories and returns a consolidation decision prompt.
// The caller should send this to an LLM and apply the result.
func FormatConsolidationPrompt(memories []*Memory) string {
	var b strings.Builder
	b.WriteString("You are consolidating similar memories. Given these memories, decide:\n")
	b.WriteString("- MERGE: combine into one memory with all important information\n")
	b.WriteString("- KEEP_BEST: keep only the most complete/accurate one\n")
	b.WriteString("- DELETE_REDUNDANT: the newer memory supersedes the older one(s)\n\n")
	for i, m := range memories {
		fmt.Fprintf(&b, "Memory %d: [%s] %s\n%s\n\n", i+1, m.Name, m.Description, m.Content)
	}
	b.WriteString("Reply with ONLY one of: MERGE, KEEP_BEST, or DELETE_REDUNDANT\n")
	b.WriteString("If MERGE, also provide the merged content after a blank line.")
	return b.String()
}

// ParseConsolidationDecision parses an LLM response into a structured decision.
// Expects the first line to contain MERGE, KEEP_BEST, or DELETE_REDUNDANT.
// For MERGE, any text after a blank line is treated as the merged content.
func ParseConsolidationDecision(response string) ConsolidationDecision {
	response = strings.TrimSpace(response)
	if response == "" {
		return ConsolidationDecision{Action: "delete_redundant"}
	}

	upper := strings.ToUpper(response)

	// Check for action keywords in the first line.
	firstLine := strings.SplitN(response, "\n", 2)[0]
	firstLineUpper := strings.ToUpper(firstLine)

	var action string
	switch {
	case strings.Contains(firstLineUpper, "MERGE"):
		action = "merge"
	case strings.Contains(firstLineUpper, "KEEP_BEST") || strings.Contains(firstLineUpper, "KEEP BEST"):
		action = "keep_best"
	case strings.Contains(firstLineUpper, "DELETE_REDUNDANT") || strings.Contains(firstLineUpper, "DELETE REDUNDANT"):
		action = "delete_redundant"
	default:
		// Try full response for action detection.
		switch {
		case strings.Contains(upper, "MERGE"):
			action = "merge"
		case strings.Contains(upper, "KEEP_BEST") || strings.Contains(upper, "KEEP BEST"):
			action = "keep_best"
		default:
			action = "delete_redundant"
		}
	}

	// For merge, extract content after blank line.
	var mergedContent string
	if action == "merge" {
		parts := strings.SplitN(response, "\n\n", 2)
		if len(parts) > 1 {
			mergedContent = strings.TrimSpace(parts[1])
		}
	}

	return ConsolidationDecision{
		Action: action,
		Result: mergedContent,
	}
}

// MemoryCluster represents a group of similar memories for consolidation.
type MemoryCluster struct {
	Key        string    // cluster identifier
	Members    []*Memory // memories in this cluster
	Similarity float64   // average pairwise similarity
}
