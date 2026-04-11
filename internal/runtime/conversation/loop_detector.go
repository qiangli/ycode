package conversation

// LoopDetector detects when the agent is stuck in a repetitive loop.
// Inspired by Cline (soft=3, hard=5) and OpenHands (AgentStuckInLoopError).
//
// It tracks recent assistant responses and detects similarity.
// When the soft threshold is hit, a warning is injected.
// When the hard threshold is hit, the loop is broken.
type LoopDetector struct {
	// recentResponses stores the last N assistant text outputs.
	recentResponses []string
	// maxTracked is how many responses to keep.
	maxTracked int
}

const (
	// LoopSoftThreshold triggers a warning injection.
	LoopSoftThreshold = 3
	// LoopHardThreshold triggers loop termination.
	LoopHardThreshold = 5
	// loopSimilarityThreshold is the minimum ratio of matching chars for "similar".
	loopSimilarityThreshold = 0.85
	// maxResponseTrackLen limits comparison length to avoid expensive comparisons.
	maxResponseTrackLen = 500
)

// NewLoopDetector creates a new loop detector.
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{
		maxTracked: LoopHardThreshold + 1,
	}
}

// LoopStatus is the result of checking for loops.
type LoopStatus int

const (
	LoopNone    LoopStatus = iota // No loop detected
	LoopWarning                   // Soft threshold — inject warning
	LoopBreak                     // Hard threshold — break the loop
)

// String returns a human-readable status.
func (s LoopStatus) String() string {
	switch s {
	case LoopWarning:
		return "warning"
	case LoopBreak:
		return "break"
	default:
		return "none"
	}
}

// Record adds an assistant response and returns the loop status.
func (d *LoopDetector) Record(response string) LoopStatus {
	// Truncate for comparison efficiency.
	tracked := response
	if len(tracked) > maxResponseTrackLen {
		tracked = tracked[:maxResponseTrackLen]
	}

	d.recentResponses = append(d.recentResponses, tracked)

	// Keep only maxTracked entries.
	if len(d.recentResponses) > d.maxTracked {
		d.recentResponses = d.recentResponses[len(d.recentResponses)-d.maxTracked:]
	}

	// Count consecutive similar responses from the end.
	consecutiveSimilar := 1
	if len(d.recentResponses) >= 2 {
		latest := d.recentResponses[len(d.recentResponses)-1]
		for i := len(d.recentResponses) - 2; i >= 0; i-- {
			if isSimilar(latest, d.recentResponses[i]) {
				consecutiveSimilar++
			} else {
				break
			}
		}
	}

	switch {
	case consecutiveSimilar >= LoopHardThreshold:
		return LoopBreak
	case consecutiveSimilar >= LoopSoftThreshold:
		return LoopWarning
	default:
		return LoopNone
	}
}

// Reset clears the detection state (e.g., after user input).
func (d *LoopDetector) Reset() {
	d.recentResponses = nil
}

// isSimilar checks if two strings are similar enough to indicate a loop.
// Uses a simple character-level ratio comparison.
func isSimilar(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if a == b {
		return true
	}

	// Quick length check — if lengths differ by >30%, not similar.
	longer, shorter := len(a), len(b)
	if shorter > longer {
		longer, shorter = shorter, longer
	}
	if float64(shorter)/float64(longer) < 0.7 {
		return false
	}

	// Simple matching: count common characters at same positions.
	minLen := shorter
	matches := 0
	for i := range minLen {
		if i < len(a) && i < len(b) && a[i] == b[i] {
			matches++
		}
	}

	ratio := float64(matches) / float64(longer)
	return ratio >= loopSimilarityThreshold
}
