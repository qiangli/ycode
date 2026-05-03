package autoloop

import (
	"log/slog"
)

// StallAction describes what should happen when a stall is detected.
type StallAction int

const (
	// StallContinue means stall was not detected; continue normally.
	StallContinue StallAction = iota
	// StallReplan means stall was detected; trigger replanning.
	// Inspired by AutoGen MagenticOne's dual-loop: when the inner loop
	// stalls, the outer loop refreshes facts and plan from conversation.
	StallReplan
	// StallAbort means stall limit exceeded even after replanning; abort.
	StallAbort
)

// StallDetector tracks consecutive turns with no progress and decides
// whether to replan or abort. This extends ycode's existing stagnation
// detection with a structured replanning response instead of just stopping.
type StallDetector struct {
	// MaxStalls is the number of consecutive stalls before triggering replan.
	// Default: 3.
	MaxStalls int

	// MaxReplans is the maximum number of replanning attempts before aborting.
	// Default: 2.
	MaxReplans int

	// Internal state.
	consecutiveStalls int
	replanCount       int
	lastScore         float64
	initialized       bool

	logger *slog.Logger
}

// NewStallDetector creates a stall detector with the given thresholds.
func NewStallDetector(maxStalls, maxReplans int) *StallDetector {
	if maxStalls <= 0 {
		maxStalls = 3
	}
	if maxReplans <= 0 {
		maxReplans = 2
	}
	return &StallDetector{
		MaxStalls:  maxStalls,
		MaxReplans: maxReplans,
		logger:     slog.Default(),
	}
}

// Observe records a new evaluation score and returns the recommended action.
func (d *StallDetector) Observe(score float64) StallAction {
	if !d.initialized {
		d.lastScore = score
		d.initialized = true
		return StallContinue
	}

	if score == d.lastScore {
		d.consecutiveStalls++
		d.logger.Info("stall detector: no progress",
			"consecutive_stalls", d.consecutiveStalls,
			"score", score,
			"replan_count", d.replanCount,
		)
	} else {
		d.consecutiveStalls = 0
	}
	d.lastScore = score

	if d.consecutiveStalls >= d.MaxStalls {
		d.consecutiveStalls = 0 // reset for next cycle

		if d.replanCount >= d.MaxReplans {
			d.logger.Info("stall detector: max replans exceeded, aborting",
				"replan_count", d.replanCount,
			)
			return StallAbort
		}

		d.replanCount++
		d.logger.Info("stall detector: triggering replan",
			"replan_count", d.replanCount,
			"max_replans", d.MaxReplans,
		)
		return StallReplan
	}

	return StallContinue
}

// Reset clears all internal state.
func (d *StallDetector) Reset() {
	d.consecutiveStalls = 0
	d.replanCount = 0
	d.lastScore = 0
	d.initialized = false
}

// Stats returns the current stall detector statistics.
func (d *StallDetector) Stats() StallStats {
	return StallStats{
		ConsecutiveStalls: d.consecutiveStalls,
		ReplanCount:       d.replanCount,
		LastScore:         d.lastScore,
	}
}

// StallStats holds stall detector statistics for observability.
type StallStats struct {
	ConsecutiveStalls int
	ReplanCount       int
	LastScore         float64
}
