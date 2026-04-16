package session

// ContextBudget calculates model-aware context thresholds.
// Inspired by Cline's context-window-utils.ts which reserves 27-40K tokens
// depending on model context window size.
type ContextBudget struct {
	// ContextWindow is the model's total context window in tokens.
	ContextWindow int
	// ReservedTokens is the number of tokens reserved for system prompt + response.
	ReservedTokens int
	// CompactionThreshold is the token count at which compaction triggers.
	CompactionThreshold int
}

// DefaultContextBudget returns the default budget (100K threshold, matching existing behavior).
func DefaultContextBudget() ContextBudget {
	return ContextBudget{
		ContextWindow:       200_000,
		ReservedTokens:      40_000,
		CompactionThreshold: CompactionThreshold, // 100K
	}
}

// ContextBudgetForModel calculates appropriate thresholds for a given model's
// context window. This follows Cline's pattern of reserving proportional tokens.
//
// | Context Window | Reserved | Compaction At |
// |---------------|----------|---------------|
// | ≤ 32K         | 8K       | 20K           |
// | 64K           | 16K      | 40K           |
// | 128K          | 30K      | 80K           |
// | 200K          | 40K      | 100K          |
// | ≥ 200K        | 20%      | 50% of window |
func ContextBudgetForModel(contextWindow int) ContextBudget {
	if contextWindow <= 0 {
		return DefaultContextBudget()
	}

	var reserved int
	switch {
	case contextWindow <= 32_000:
		reserved = 8_000
	case contextWindow <= 64_000:
		reserved = 16_000
	case contextWindow <= 128_000:
		reserved = 30_000
	case contextWindow <= 200_000:
		reserved = 40_000
	default:
		reserved = contextWindow / 5 // 20% for very large windows
	}

	// Compaction threshold = halfway between reserved and total.
	compactionAt := (contextWindow - reserved) / 2
	compactionAt = max(compactionAt, 10_000)

	return ContextBudget{
		ContextWindow:       contextWindow,
		ReservedTokens:      reserved,
		CompactionThreshold: compactionAt,
	}
}

// NonCachingCompactionDiscount is applied to the compaction threshold for
// providers without prompt caching. Since every token is billed at full price
// every turn, we compact earlier to keep costs down.
const NonCachingCompactionDiscount = 0.70 // 30% reduction

// ContextBudgetForProvider returns a budget adjusted for provider capabilities.
// Non-caching providers (OpenAI, Gemini, Moonshot/Kimi) get a lower compaction
// threshold since they pay full price for every input token every turn.
func ContextBudgetForProvider(contextWindow int, cachingSupported bool) ContextBudget {
	budget := ContextBudgetForModel(contextWindow)
	if !cachingSupported {
		budget.CompactionThreshold = max(
			int(float64(budget.CompactionThreshold)*NonCachingCompactionDiscount),
			10_000,
		)
	}
	return budget
}

// EffectiveMax returns the maximum tokens available for conversation messages
// (context window minus reserved).
func (b ContextBudget) EffectiveMax() int {
	return b.ContextWindow - b.ReservedTokens
}

// SoftTrimAt returns the token count at which soft trimming should begin.
func (b ContextBudget) SoftTrimAt() int {
	return int(float64(b.CompactionThreshold) * SoftTrimRatio)
}

// HardClearAt returns the token count at which hard clearing should begin.
func (b ContextBudget) HardClearAt() int {
	return int(float64(b.CompactionThreshold) * HardClearRatio)
}
