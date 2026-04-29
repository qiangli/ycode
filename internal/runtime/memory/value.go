package memory

import (
	"math"
	"time"
)

// DefaultRewardAlpha is the default exponential moving average weight for reward propagation.
const DefaultRewardAlpha = 0.3

// UpdateValueOnAccess increments access tracking fields on a memory.
// Called when a memory is returned from Recall.
func UpdateValueOnAccess(mem *Memory) {
	mem.AccessCount++
	mem.LastAccessedAt = time.Now()
}

// PropagateReward updates a memory's ValueScore using exponential moving average.
// reward should be in [0, 1]. alpha controls how much weight the new reward gets
// (higher alpha = more responsive to recent feedback).
func PropagateReward(mem *Memory, reward float64, alpha float64) {
	if alpha <= 0 || alpha > 1 {
		alpha = DefaultRewardAlpha
	}
	// Clamp reward to [0, 1].
	if reward < 0 {
		reward = 0
	}
	if reward > 1 {
		reward = 1
	}

	current := mem.EffectiveValue()
	mem.ValueScore = alpha*reward + (1-alpha)*current
}

// DecayValue applies temporal decay to ValueScore for memories not accessed recently.
// halfLifeDays controls how quickly unused memories lose value.
func DecayValue(mem *Memory, halfLifeDays float64) {
	if halfLifeDays <= 0 {
		halfLifeDays = 30
	}

	// Use last accessed time if available, else updated time.
	reference := mem.LastAccessedAt
	if reference.IsZero() {
		reference = mem.UpdatedAt
	}
	if reference.IsZero() {
		return
	}

	daysSince := time.Since(reference).Hours() / 24.0
	if daysSince <= 7 {
		return // grace period
	}

	decay := math.Exp(-0.693 * daysSince / halfLifeDays) // ln(2) ≈ 0.693
	mem.ValueScore = mem.EffectiveValue() * decay
}

// EffectiveValue returns the memory's dynamic value, falling back to Importance.
func (m *Memory) EffectiveValue() float64 {
	if m.ValueScore > 0 {
		return m.ValueScore
	}
	if m.Importance > 0 {
		return m.Importance
	}
	return 0.5 // default
}
