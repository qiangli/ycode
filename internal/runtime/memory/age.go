package memory

import "time"

// StalenessThresholds define when memories become stale.
var StalenessThresholds = map[Type]time.Duration{
	TypeProject:   30 * 24 * time.Hour,  // 30 days
	TypeReference: 90 * 24 * time.Hour,  // 90 days
	TypeUser:      180 * 24 * time.Hour, // 180 days
	TypeFeedback:  365 * 24 * time.Hour, // 1 year
}

// IsStale checks if a memory has exceeded its staleness threshold
// or is past its temporal validity window.
func IsStale(mem *Memory) bool {
	// A memory with a ValidUntil in the past is always stale.
	if mem.ValidUntil != nil && time.Now().After(*mem.ValidUntil) {
		return true
	}

	threshold, ok := StalenessThresholds[mem.Type]
	if !ok {
		threshold = 90 * 24 * time.Hour // default 90 days
	}
	return time.Since(mem.UpdatedAt) > threshold
}

// DecayScore applies temporal decay to a relevance score.
func DecayScore(score float64, mem *Memory) float64 {
	age := time.Since(mem.UpdatedAt)
	days := age.Hours() / 24.0
	if days <= 7 {
		return score // no decay for first week
	}
	// Logarithmic decay after first week.
	decay := 1.0 / (1.0 + days/30.0)
	return score * decay
}
