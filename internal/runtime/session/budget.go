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
	// MaxChatHistoryTokens caps the size of summarized chat history.
	// Inspired by aider's formula: min(max(input_tokens/16, 1024), 8192).
	// This prevents history from dominating the context, especially for
	// non-caching providers where every token costs full price.
	MaxChatHistoryTokens int

	// ReservedBuffer is tokens reserved as safety margin against overflow.
	// When current_tokens + ReservedBuffer >= ContextWindow - ReservedTokens,
	// compaction triggers even if CompactionThreshold hasn't been reached.
	// Inspired by Kimi CLI's dual compaction trigger.
	ReservedBuffer int
}

// ShouldCompact implements Kimi CLI's dual compaction trigger:
//  1. Ratio-based: currentTokens >= CompactionThreshold
//  2. Reserved-buffer: currentTokens + ReservedBuffer >= ContextWindow - ReservedTokens
//
// Returns true when either condition fires.
func (b ContextBudget) ShouldCompact(currentTokens int) bool {
	return currentTokens >= b.CompactionThreshold ||
		currentTokens+b.ReservedBuffer >= b.ContextWindow-b.ReservedTokens
}

// UnknownModelContextWindow is what we assume when a model does not tell us its
// window. It is deliberately SMALL.
//
// The old default assumed 200_000 — the largest window we support — and called it
// "conservative". It is the opposite: an unknown model is the one case where being
// wrong is unrecoverable, and assuming the biggest possible window means happily
// packing 100K tokens into a local 8K model and getting a wall of API errors. Guess
// low and the cost is some avoidable compaction. Guess high and nothing works.
const UnknownModelContextWindow = 32_000

// DefaultContextBudget returns the budget for a model that did not advertise a
// window. See UnknownModelContextWindow for why it is small rather than large.
func DefaultContextBudget() ContextBudget {
	return ContextBudgetForModel(UnknownModelContextWindow)
}

// chatHistoryBudget calculates the max chat history tokens for a given context window.
// Formula from aider: min(max(input_tokens/16, 1024), 8192).
func chatHistoryBudget(contextWindow int) int {
	budget := contextWindow / 16
	budget = max(budget, 1024)
	budget = min(budget, 8192)
	return budget
}

// chatHistoryBudgetAggressive calculates a tighter history budget for non-caching providers.
func chatHistoryBudgetAggressive(contextWindow int) int {
	budget := contextWindow / 24
	budget = max(budget, 1024)
	budget = min(budget, 4096)
	return budget
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

	// The table above was written for the big windows and never checked at the small
	// end. An 8K window hits the first case and reserves 8_000 — the ENTIRE context.
	// Usable becomes zero, every threshold below is nonsense, and nothing says so.
	//
	// Cap the reserve at a quarter of the window. A reserve is a safety margin; a
	// safety margin that consumes the thing it protects is not one.
	if maxReserve := contextWindow / 4; reserved > maxReserve {
		reserved = maxReserve
	}

	// Compaction threshold = halfway between reserved and total.
	compactionAt := (contextWindow - reserved) / 2

	// The 10_000 floor has the same disease as the reserve: on a 16K window it raises
	// compaction from 4_000 to 10_000, which is ABOVE the 8_000 that window can
	// actually hold. A floor that lifts the trigger past the cliff never fires.
	//
	// Clamp it to the usable window. A threshold outside the window is not
	// conservative — it is inert, and it reports "healthy" until the API says no.
	compactionAt = max(compactionAt, 10_000)
	if usable := contextWindow - reserved; compactionAt >= usable {
		compactionAt = usable * 3 / 4
	}

	return ContextBudget{
		ContextWindow:        contextWindow,
		ReservedTokens:       reserved,
		CompactionThreshold:  compactionAt,
		MaxChatHistoryTokens: chatHistoryBudget(contextWindow),
		ReservedBuffer:       contextWindow / 10, // 10% safety margin
	}
}

// WithReserved applies an operator's explicit reserve, and re-derives the thresholds
// under it. The reserve is a consequence of the request WE choose to make, not a fact
// about the model — so it is the one number an operator has standing to set.
//
// It is still clamped: a reserve that eats its own window is not a setting, it is a
// broken config, and honouring it literally would just move the failure somewhere
// harder to see.
func (b ContextBudget) WithReserved(reserved int) ContextBudget {
	if reserved <= 0 || b.ContextWindow <= 0 {
		return b
	}
	if maxReserve := b.ContextWindow / 2; reserved > maxReserve {
		reserved = maxReserve
	}
	b.ReservedTokens = reserved
	if usable := b.EffectiveMax(); b.CompactionThreshold >= usable {
		b.CompactionThreshold = usable * 3 / 4
	}
	return b
}

// MaxResponseTokens is the largest max_tokens it is SENSIBLE to ask this model for.
//
// max_tokens is a request parameter, not a model property, and it was a flat 32_000
// no matter what was on the other end. On a 32K model that asks for a reply the size
// of the entire context window: there is no conversation you could have alongside it,
// and no reserve that could hold it. You cannot reserve your way out of asking for
// too much — you have to ask for less.
//
// A third of the window, capped at the caller's own ceiling.
func (b ContextBudget) MaxResponseTokens(ceiling int) int {
	if b.ContextWindow <= 0 {
		return ceiling
	}
	want := b.ContextWindow / 3
	if ceiling > 0 && want > ceiling {
		want = ceiling
	}
	return max(want, 1_024)
}

// WithResponseReserve guarantees the budget sets aside at least maxOutputTokens for
// the model's reply, and re-derives every threshold under it.
//
// The reserve is not a property of the model. It is a property of the REQUEST: it
// exists to hold the response we are about to ask for. And it was not tied to it —
// the table reserves 30K on a 128K window while MaxOutputTokenCap asks for up to 32K.
// Fill the usable 98K, request a 32K reply, and that is 130K against a 128K window.
// The reserve was short by exactly the thing it was reserving for.
//
// Kept separate from ContextBudgetForProvider because only the caller knows what
// max_tokens it is actually going to send. A constant copied into this package would
// be a second source of truth, and it would drift.
func (b ContextBudget) WithResponseReserve(maxOutputTokens int) ContextBudget {
	if maxOutputTokens <= 0 || b.ContextWindow <= 0 {
		return b
	}
	// A little headroom above the response itself: the reserve also absorbs the
	// tokenizer's disagreement with our 4-chars-per-token estimate.
	want := maxOutputTokens + maxOutputTokens/10
	if want <= b.ReservedTokens {
		return b
	}
	// Never let the reserve eat the window (same rule as ContextBudgetForModel).
	if maxReserve := b.ContextWindow / 2; want > maxReserve {
		want = maxReserve
	}
	b.ReservedTokens = want

	usable := b.EffectiveMax()
	if b.CompactionThreshold >= usable {
		b.CompactionThreshold = usable * 3 / 4
	}
	return b
}

// MaskingProtectionBudget is the token budget of RECENT tool observations that
// observation masking must never touch, and MaskingMinPrunable the volume of older
// ones it takes before masking is worth doing at all.
//
// Both used to be absolute constants (50K / 30K). On a 32K model — 24K usable — a
// 30K protection budget exceeds the entire window, so no observation is ever
// classifiable as prunable and masking silently never runs. Same disease as the
// compaction threshold: an absolute number measured against a relative window.
//
// Derive them from the window and they mean the same thing on every model.
func (b ContextBudget) MaskingProtectionBudget() int {
	return max(b.EffectiveMax()*40/100, 2_000)
}

func (b ContextBudget) MaskingMinPrunable() int {
	return max(b.EffectiveMax()*25/100, 1_000)
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
		// A DISCOUNT may only lower the threshold. The previous version wrapped this in
		// max(..., 10_000), so on a small window it RAISED it: 4_500 * 0.70 = 3_150,
		// floored back up to 10_000 — above a 6_000 usable window, and inert. The same
		// 10_000 floor that broke ContextBudgetForModel, in the function that is
		// supposed to be making the budget tighter.
		discounted := int(float64(budget.CompactionThreshold) * NonCachingCompactionDiscount)
		if discounted < budget.CompactionThreshold {
			budget.CompactionThreshold = discounted
		}
		budget.MaxChatHistoryTokens = chatHistoryBudgetAggressive(contextWindow)
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
