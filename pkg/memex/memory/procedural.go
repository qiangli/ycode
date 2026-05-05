package memory

import (
	"fmt"
	"strings"
	"time"
)

// ProceduralPattern describes a learned workflow or decision-making heuristic.
type ProceduralPattern struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	Context     string   `json:"context"`   // when this pattern applies
	Rationale   string   `json:"rationale"` // why this pattern works
	Source      string   `json:"source"`    // how it was learned (manual, dreaming, etc.)
}

// NewProceduralMemory creates a procedural memory from a learned pattern.
func NewProceduralMemory(pattern *ProceduralPattern) *Memory {
	name := fmt.Sprintf("procedural_%s", sanitizeName(pattern.Name))
	description := fmt.Sprintf("Workflow pattern: %s", pattern.Description)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", pattern.Description)

	if pattern.Context != "" {
		fmt.Fprintf(&sb, "**When to apply:** %s\n\n", pattern.Context)
	}

	if len(pattern.Steps) > 0 {
		sb.WriteString("### Steps\n\n")
		for i, step := range pattern.Steps {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, step)
		}
		sb.WriteString("\n")
	}

	if pattern.Rationale != "" {
		fmt.Fprintf(&sb, "**Why:** %s\n\n", pattern.Rationale)
	}

	if pattern.Source != "" {
		fmt.Fprintf(&sb, "**Source:** %s\n", pattern.Source)
	}

	return &Memory{
		Name:        name,
		Description: description,
		Type:        TypeProcedural,
		Scope:       ScopeProject,
		Content:     sb.String(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// sanitizeName converts a pattern name to a valid memory file name.
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	// Keep only alphanumeric, underscore, hyphen.
	var result []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		return "unnamed"
	}
	return string(result)
}
