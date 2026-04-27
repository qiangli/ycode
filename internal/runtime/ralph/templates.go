package ralph

import (
	"fmt"
	"strings"
)

// GenerateIterationPrompt builds the prompt for one Ralph iteration.
func GenerateIterationPrompt(state *State, progressExcerpt string, story *Story) string {
	var b strings.Builder

	b.WriteString("You are an autonomous coding agent working on a specific task.\n\n")

	if story != nil {
		fmt.Fprintf(&b, "## Current Story: %s\n", story.Title)
		fmt.Fprintf(&b, "**ID**: %s\n", story.ID)
		fmt.Fprintf(&b, "**Description**: %s\n\n", story.Description)
		if len(story.AcceptanceCriteria) > 0 {
			b.WriteString("**Acceptance Criteria**:\n")
			for _, ac := range story.AcceptanceCriteria {
				fmt.Fprintf(&b, "- %s\n", ac)
			}
			b.WriteString("\n")
		}
		if len(story.Notes) > 0 {
			b.WriteString("**Notes from previous iterations**:\n")
			for _, note := range story.Notes {
				fmt.Fprintf(&b, "- %s\n", note)
			}
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "## Iteration %d\n", state.Iteration+1)
	if state.LastError != "" {
		fmt.Fprintf(&b, "Previous iteration error: %s\n", state.LastError)
	}
	if state.LastCheckOutput != "" {
		fmt.Fprintf(&b, "Previous check output: %s\n", state.LastCheckOutput)
	}

	if progressExcerpt != "" {
		b.WriteString("\n## Learnings from previous iterations\n")
		b.WriteString(progressExcerpt)
		b.WriteString("\n")
	}

	b.WriteString("\n## Instructions\n")
	b.WriteString("1. Implement the changes needed for this story\n")
	b.WriteString("2. Run any relevant tests to verify\n")
	b.WriteString("3. If tests pass, the iteration succeeds\n")
	b.WriteString("4. Document any learnings or patterns discovered\n")

	return b.String()
}

// FormatKnowledgeEntry creates a knowledge entry for AGENTS.md/CLAUDE.md seeding.
func FormatKnowledgeEntry(storyID, pattern string) string {
	return fmt.Sprintf("- [%s] %s", storyID, pattern)
}
