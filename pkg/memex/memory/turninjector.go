package memory

import (
	"context"
	"fmt"
	"strings"
)

// TurnInjector provides per-turn context-aware memory retrieval.
// It runs a recall query on the user's message and returns relevant
// memories formatted for injection as a system message.
type TurnInjector struct {
	manager   *Manager
	budget    int    // max characters for injected context
	lastQuery string // previous turn's query for dedup
}

// NewTurnInjector creates a turn injector with the given character budget.
func NewTurnInjector(manager *Manager, budget int) *TurnInjector {
	if budget <= 0 {
		budget = 1500
	}
	return &TurnInjector{
		manager: manager,
		budget:  budget,
	}
}

// InjectForTurn returns memories relevant to the user's message, formatted
// as a memory-context block. Returns empty string if no relevant memories found
// or if the query is too similar to the previous turn (dedup).
func (ti *TurnInjector) InjectForTurn(ctx context.Context, userMessage string) string {
	if userMessage == "" {
		return ""
	}

	// Dedup: skip if very similar to the previous turn's query.
	if ti.lastQuery != "" {
		sim := jaccardSimilarity(
			wordSet(ti.lastQuery),
			wordSet(userMessage),
		)
		if sim >= 0.8 {
			return "" // too similar, skip re-injection
		}
	}
	ti.lastQuery = userMessage

	results, err := ti.manager.Recall(userMessage, 3)
	if err != nil || len(results) == 0 {
		return ""
	}

	// Format results within budget.
	var b strings.Builder
	b.WriteString("<memory-context>\n")
	chars := 0

	for _, r := range results {
		entry := fmt.Sprintf("**%s** (%s): %s\n", r.Memory.Name, r.Memory.Type, r.Memory.Description)
		if r.Memory.Content != "" {
			content := r.Memory.Content
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			entry += content + "\n"
		}
		entry += "\n"

		if chars+len(entry) > ti.budget {
			break
		}
		b.WriteString(entry)
		chars += len(entry)

		// Update access tracking.
		UpdateValueOnAccess(r.Memory)
	}

	b.WriteString("</memory-context>")

	if chars == 0 {
		return ""
	}

	return b.String()
}

// wordSet returns lowercased word set for a string, stripping punctuation.
func wordSet(s string) map[string]struct{} {
	words := strings.Fields(strings.ToLower(s))
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}") // strip trailing punctuation
		if w != "" {
			set[w] = struct{}{}
		}
	}
	return set
}
