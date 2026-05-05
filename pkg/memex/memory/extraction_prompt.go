package memory

import (
	"fmt"
	"strings"
)

// buildExtractionSystemPrompt returns the system prompt for memory extraction.
func buildExtractionSystemPrompt() string {
	return `You are a memory extraction engine for a software development assistant.
Your job is to identify facts worth remembering from a conversation between a user and an AI assistant.

## Rules

1. Extract ONLY genuinely memorable facts — things that would be useful in future conversations.
2. Use ADD-only semantics: create new memory entries, never update or delete existing ones.
3. Resolve temporal references against the Observation Date (not the current date):
   - "yesterday" → the day before Observation Date
   - "last week" → the week preceding Observation Date
   - "recently" → shortly before Observation Date
4. Preserve specificity: exact file paths, error messages, version numbers, quantities.
5. Capture transitions: "switched from X to Y because..." not just "uses Y".
6. Make each fact self-contained: replace pronouns with names/descriptions.
7. Skip facts already captured in the Existing Memories list (deduplication).
8. Skip trivial or ephemeral facts (greetings, acknowledgments, tool outputs).

## Categories of Memorable Facts

- User preferences (coding style, tool preferences, communication style)
- Architecture decisions and their rationale
- Bug patterns and their fixes
- Project conventions and constraints
- Technology choices and trade-offs
- Key people and their roles
- Recurring problems or pain points

## Response Format

Respond with a JSON object:
` + "```json" + `
{
  "facts": [
    {
      "text": "The user prefers snake_case for Go test function names",
      "attributed_to": "user",
      "temporal_anchor": "",
      "entities": ["Go"],
      "confidence": 0.9
    }
  ]
}
` + "```" + `

If no facts are worth extracting, return: {"facts": []}
Set confidence between 0.0 and 1.0 based on how certain you are the fact is correct and worth remembering.`
}

// buildExtractionUserPrompt builds the user prompt with conversation context.
func buildExtractionUserPrompt(ctx ExtractionContext, existingSnippets []string) string {
	var b strings.Builder

	// Observation date for temporal grounding.
	b.WriteString(fmt.Sprintf("**Observation Date**: %s\n\n", ctx.ObservationDate.Format("2006-01-02")))

	// Existing memories for dedup.
	if len(existingSnippets) > 0 {
		b.WriteString("**Existing Memories** (do NOT extract duplicates of these):\n")
		for _, s := range existingSnippets {
			b.WriteString(s + "\n")
		}
		b.WriteString("\n")
	}

	// Recent messages for pronoun resolution.
	if len(ctx.RecentMessages) > 0 {
		b.WriteString("**Recent Context** (for pronoun resolution only, do NOT extract from these):\n")
		for _, m := range ctx.RecentMessages {
			content := m.Content
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			b.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, content))
		}
		b.WriteString("\n")
	}

	// New messages to extract from.
	b.WriteString("**New Messages** (extract facts from these):\n")
	for _, m := range ctx.NewMessages {
		b.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, m.Content))
	}

	return b.String()
}
