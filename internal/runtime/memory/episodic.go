package memory

import (
	"fmt"
	"strings"
	"time"
)

// EpisodicMetadata describes the context of a specific agent experience.
type EpisodicMetadata struct {
	AgentType   string        `json:"agent_type"`
	AgentID     string        `json:"agent_id"`
	TaskSummary string        `json:"task_summary"`
	ToolsUsed   []string      `json:"tools_used"`
	Duration    time.Duration `json:"duration"`
	Success     bool          `json:"success"`
	Learnings   string        `json:"learnings"` // LLM-generated reflection (optional)
	SessionID   string        `json:"session_id"`
}

// NewEpisodicMemory creates an episodic memory from agent completion metadata.
func NewEpisodicMemory(meta *EpisodicMetadata) *Memory {
	name := fmt.Sprintf("episodic_%s_%s", meta.AgentType, time.Now().Format("20060102_150405"))
	description := fmt.Sprintf("Agent %s experience: %s", meta.AgentType, truncate(meta.TaskSummary, 80))

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Agent Experience\n\n")
	fmt.Fprintf(&sb, "- **Agent Type:** %s\n", meta.AgentType)
	fmt.Fprintf(&sb, "- **Agent ID:** %s\n", meta.AgentID)
	fmt.Fprintf(&sb, "- **Task:** %s\n", meta.TaskSummary)
	fmt.Fprintf(&sb, "- **Duration:** %s\n", meta.Duration.Round(time.Second))
	fmt.Fprintf(&sb, "- **Success:** %v\n", meta.Success)
	if len(meta.ToolsUsed) > 0 {
		fmt.Fprintf(&sb, "- **Tools Used:** %s\n", strings.Join(meta.ToolsUsed, ", "))
	}
	if meta.Learnings != "" {
		fmt.Fprintf(&sb, "\n### Learnings\n\n%s\n", meta.Learnings)
	}

	return &Memory{
		Name:        name,
		Description: description,
		Type:        TypeEpisodic,
		Scope:       ScopeProject,
		Content:     sb.String(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
