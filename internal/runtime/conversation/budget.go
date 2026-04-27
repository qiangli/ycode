package conversation

// IterationBudget tracks the remaining iteration budget for an agent loop.
// It allows Total normal iterations plus one grace call where the agent
// is asked to summarize and wrap up.
type IterationBudget struct {
	Total     int  // total iterations allowed (not counting grace)
	Used      int  // iterations consumed so far
	GraceUsed bool // true if the grace call has been taken
}

// NewIterationBudget creates a budget with the given total.
// If total is <= 0, it defaults to 1 to ensure at least one iteration.
func NewIterationBudget(total int) *IterationBudget {
	if total <= 0 {
		total = 1
	}
	return &IterationBudget{Total: total}
}

// Consume records one iteration. Returns true if the iteration was allowed.
// After Total normal iterations are consumed, one additional grace call is
// permitted. After that, Consume returns false.
func (b *IterationBudget) Consume() bool {
	if b.Used < b.Total {
		b.Used++
		return true
	}
	if !b.GraceUsed {
		b.GraceUsed = true
		b.Used++
		return true
	}
	return false
}

// Remaining returns the number of normal iterations left (not counting grace).
func (b *IterationBudget) Remaining() int {
	r := b.Total - b.Used
	if r < 0 {
		return 0
	}
	return r
}

// IsExhausted returns true if all iterations (including grace) are spent.
func (b *IterationBudget) IsExhausted() bool {
	return b.Used > b.Total && b.GraceUsed
}

// IsGrace returns true if the grace call has been taken.
func (b *IterationBudget) IsGrace() bool {
	return b.GraceUsed
}

// GraceMessage returns the system message to inject on the grace turn.
func (b *IterationBudget) GraceMessage() string {
	return "You have exhausted your iteration budget. This is your final turn. " +
		"Summarize your progress, state any remaining work, and provide your best answer."
}
