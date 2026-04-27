package rollout

// ResultBudget controls tool result size in training rollouts.
type ResultBudget struct {
	DefaultMaxChars int            // default per-tool result limit
	TurnBudgetChars int            // aggregate budget per turn
	PreviewChars    int            // inline preview size after persistence
	Overrides       map[string]int // per-tool overrides (tool name -> max chars)
}

// DefaultBudget returns sensible defaults for training.
func DefaultBudget() *ResultBudget {
	return &ResultBudget{
		DefaultMaxChars: 4000,
		TurnBudgetChars: 16000,
		PreviewChars:    500,
		Overrides: map[string]int{
			"read_file": 0, // unlimited — always inline
			"terminal":  10000,
		},
	}
}

// MaxCharsForTool returns the max result size for a tool.
func (b *ResultBudget) MaxCharsForTool(toolName string) int {
	if override, ok := b.Overrides[toolName]; ok {
		return override
	}
	return b.DefaultMaxChars
}

// TruncateResult truncates a tool result to the budget limit.
func (b *ResultBudget) TruncateResult(toolName, result string) string {
	maxChars := b.MaxCharsForTool(toolName)
	if maxChars <= 0 || len(result) <= maxChars {
		return result
	}
	return result[:b.PreviewChars] + "\n[... truncated, full result persisted ...]"
}
