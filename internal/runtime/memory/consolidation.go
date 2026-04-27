package memory

import (
	"fmt"
	"strings"
)

// ConsolidationDecision represents the LLM's decision about similar memories.
type ConsolidationDecision struct {
	Action string // "merge", "keep_best", "delete_redundant"
	Result string // merged content or selected memory name
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
