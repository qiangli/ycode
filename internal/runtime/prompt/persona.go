package prompt

import (
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

// MaxPersonaBudget is the maximum character budget for persona context in the prompt.
const MaxPersonaBudget = 400

// PersonaSection renders persona context as actionable LLM directives.
// The output is scaled by the persona's confidence — at low confidence,
// less context is injected to avoid over-personalization.
// Returns empty string if persona is nil or has no useful signal.
func PersonaSection(p *memory.Persona) string {
	if p == nil {
		return ""
	}

	// Scale budget by confidence.
	budget := int(float64(MaxPersonaBudget) * p.Confidence)
	if budget < 50 {
		return "" // too little confidence to inject anything useful
	}

	var parts []string

	// Knowledge summary.
	if p.Knowledge != nil && len(p.Knowledge.Domains) > 0 {
		if ks := formatKnowledgeSummary(p.Knowledge); ks != "" {
			parts = append(parts, ks)
		}
	}

	// Communication directives.
	if p.Communication != nil && p.Communication.Confidence > 0.2 {
		if cd := formatCommunicationDirective(p.Communication); cd != "" {
			parts = append(parts, cd)
		}
	}

	// Session context (ephemeral).
	if p.SessionContext != nil && p.SessionContext.DetectedRole != "" {
		parts = append(parts, fmt.Sprintf("Current session: %s mode (%s).",
			p.SessionContext.DetectedRole, p.SessionContext.DetectedMood))
	}

	// Top observations.
	if p.Interactions != nil {
		if obs := formatTopObservations(p.Interactions); obs != "" {
			parts = append(parts, obs)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	// Build the section, respecting budget.
	header := "# User context\n"
	content := strings.Join(parts, "\n")

	full := header + content
	if len(full) > budget {
		full = full[:budget]
		// Trim to last complete line.
		if lastNL := strings.LastIndex(full, "\n"); lastNL > len(header) {
			full = full[:lastNL]
		}
	}

	return full
}

// formatKnowledgeSummary renders a one-line expertise summary.
func formatKnowledgeSummary(km *memory.KnowledgeMap) string {
	if len(km.Domains) == 0 {
		return ""
	}

	// Pick up to 4 highest-confidence domains.
	type domainInfo struct {
		name  string
		level string
		conf  float64
	}
	var infos []domainInfo
	for _, d := range km.Domains {
		infos = append(infos, domainInfo{d.Name, d.Level, d.Confidence})
	}
	// Sort by confidence descending.
	for i := range infos {
		for j := i + 1; j < len(infos); j++ {
			if infos[j].conf > infos[i].conf {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}
	if len(infos) > 4 {
		infos = infos[:4]
	}

	var parts []string
	for _, d := range infos {
		parts = append(parts, fmt.Sprintf("%s %s (%s)", d.level, d.name, d.name))
	}
	// Simplify: just list domain+level.
	var simple []string
	for _, d := range infos {
		simple = append(simple, fmt.Sprintf("%s (%s)", d.name, d.level))
	}

	return "Expertise: " + strings.Join(simple, ", ") + "."
}

// formatCommunicationDirective renders communication style as an LLM instruction.
func formatCommunicationDirective(cs *memory.CommunicationStyle) string {
	var parts []string

	if cs.Verbosity < 0.3 {
		parts = append(parts, "prefers terse, direct responses")
	} else if cs.Verbosity > 0.7 {
		parts = append(parts, "prefers detailed explanations")
	}

	if cs.Formality < 0.3 {
		parts = append(parts, "casual tone")
	} else if cs.Formality > 0.7 {
		parts = append(parts, "formal tone")
	}

	if cs.JustDoIt {
		parts = append(parts, "wants results over explanation")
	}
	if cs.AsksClarify {
		parts = append(parts, "frequently asks follow-ups")
	}

	if len(parts) == 0 {
		return ""
	}
	return "Communication: " + strings.Join(parts, "; ") + "."
}

// formatTopObservations picks the top 3 highest-confidence observations.
func formatTopObservations(is *memory.InteractionSummary) string {
	if len(is.Observations) == 0 {
		return ""
	}

	// Sort by confidence descending (copy to avoid mutation).
	sorted := make([]memory.PersonaObservation, len(is.Observations))
	copy(sorted, is.Observations)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Confidence > sorted[i].Confidence {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	limit := 3
	if len(sorted) < limit {
		limit = len(sorted)
	}

	var lines []string
	for _, obs := range sorted[:limit] {
		lines = append(lines, obs.Text)
	}
	return "Known: " + strings.Join(lines, ". ") + "."
}
