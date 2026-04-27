package tools

import (
	"fmt"
	"log/slog"
	"time"
)

// EvolutionType classifies how a skill evolved.
type EvolutionType string

const (
	EvolutionFix     EvolutionType = "fix"     // fix a broken skill
	EvolutionDerived EvolutionType = "derived" // create a variant
	EvolutionCapture EvolutionType = "capture" // brand new from execution
)

// EvolutionCandidate is a proposed skill evolution.
type EvolutionCandidate struct {
	Type          EvolutionType
	SkillName     string // target skill (empty for capture)
	ParentSkill   string // parent skill name (empty for capture)
	ParentVersion int
	Reason        string // why this evolution is needed
	SuggestedFix  string // the proposed change
}

// SkillVersion tracks evolution lineage.
type SkillVersion struct {
	Version       int           `json:"version"`
	ParentSkill   string        `json:"parent_skill,omitempty"`
	ParentVersion int           `json:"parent_version,omitempty"`
	EvolutionType EvolutionType `json:"evolution_type"`
	Reason        string        `json:"reason"`
	CreatedAt     time.Time     `json:"created_at"`
}

// EvolutionEngine analyzes execution results and proposes skill improvements.
type EvolutionEngine struct {
	// AnalyzeFunc is called to analyze execution traces and propose evolutions.
	// Typically backed by an LLM call.
	AnalyzeFunc func(toolName string, errorLog string, reliability *ToolReliability) ([]EvolutionCandidate, error)
}

// NewEvolutionEngine creates an evolution engine.
func NewEvolutionEngine() *EvolutionEngine {
	return &EvolutionEngine{}
}

// Analyze examines a tool's execution history and proposes evolutions.
func (e *EvolutionEngine) Analyze(toolName string, errorLog string, reliability *ToolReliability) ([]EvolutionCandidate, error) {
	if e.AnalyzeFunc == nil {
		return nil, fmt.Errorf("AnalyzeFunc not configured")
	}
	slog.Info("skill.evolution.analyze",
		"tool", toolName,
		"success_rate", reliability.SuccessRate,
		"total_calls", reliability.TotalCalls,
	)
	candidates, err := e.AnalyzeFunc(toolName, errorLog, reliability)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		slog.Info("skill.evolution.candidate",
			"type", string(c.Type),
			"skill", c.SkillName,
			"reason", c.Reason,
		)
	}
	return candidates, nil
}

// FormatAnalysisPrompt creates the LLM prompt for evolution analysis.
func FormatAnalysisPrompt(toolName string, errorLog string, reliability *ToolReliability) string {
	return fmt.Sprintf(`Analyze this tool's execution history and suggest improvements.

Tool: %s
Success Rate: %.1f%% (%d/%d calls)
Recent Errors:
%s

Suggest ONE of:
- FIX: if the tool has a specific bug or reliability issue, describe the fix
- DERIVED: if a variant would handle edge cases better, describe the variant
- CAPTURE: if this execution revealed a new reusable pattern, describe the new skill

Reply with the type (FIX/DERIVED/CAPTURE) and a brief description.`,
		toolName,
		reliability.SuccessRate*100,
		reliability.SuccessCount,
		reliability.TotalCalls,
		errorLog,
	)
}
