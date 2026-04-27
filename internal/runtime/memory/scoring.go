package memory

import (
	"math"
	"time"
)

// ScoringWeights controls composite recall scoring.
type ScoringWeights struct {
	Semantic   float64 // weight for semantic/keyword similarity (default 0.5)
	Recency    float64 // weight for recency decay (default 0.3)
	Importance float64 // weight for explicit importance (default 0.2)
}

// DefaultWeights returns default scoring weights.
func DefaultWeights() ScoringWeights {
	return ScoringWeights{Semantic: 0.5, Recency: 0.3, Importance: 0.2}
}

// CompositeScore computes a weighted score combining similarity, recency, and importance.
func CompositeScore(similarity float64, updatedAt time.Time, importance float64, weights ScoringWeights) float64 {
	recency := RecencyDecay(updatedAt, 30*24*time.Hour) // 30-day half-life
	if importance <= 0 {
		importance = 0.5 // default importance
	}
	return weights.Semantic*similarity + weights.Recency*recency + weights.Importance*importance
}

// RecencyDecay returns a value in [0,1] based on exponential decay.
// halfLife controls how quickly old memories fade.
func RecencyDecay(updatedAt time.Time, halfLife time.Duration) float64 {
	age := time.Since(updatedAt)
	if age <= 0 {
		return 1.0
	}
	return math.Exp(-0.693 * float64(age) / float64(halfLife)) // ln(2) ≈ 0.693
}
