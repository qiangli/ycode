package mesh

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SafetyGuard enforces limits on autonomous actions.
type SafetyGuard struct {
	mu                   sync.Mutex
	fixCount             int
	fixWindowStart       time.Time
	maxFixesPerHour      int
	maxAttemptsPerReport int
	attemptsByReport     map[string]int
	logger               *slog.Logger
}

// NewSafetyGuard creates a safety guard.
func NewSafetyGuard(maxFixesPerHour, maxAttemptsPerReport int) *SafetyGuard {
	if maxFixesPerHour <= 0 {
		maxFixesPerHour = 5
	}
	if maxAttemptsPerReport <= 0 {
		maxAttemptsPerReport = 2
	}
	return &SafetyGuard{
		maxFixesPerHour:      maxFixesPerHour,
		maxAttemptsPerReport: maxAttemptsPerReport,
		attemptsByReport:     make(map[string]int),
		fixWindowStart:       time.Now(),
		logger:               slog.Default(),
	}
}

// CanFix checks if a fix is allowed (budget not exhausted).
func (sg *SafetyGuard) CanFix(reportID string) (bool, string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	// Reset hourly window.
	if time.Since(sg.fixWindowStart) > time.Hour {
		sg.fixCount = 0
		sg.fixWindowStart = time.Now()
	}

	// Check hourly budget.
	if sg.fixCount >= sg.maxFixesPerHour {
		return false, fmt.Sprintf("fix budget exhausted (%d/%d this hour)", sg.fixCount, sg.maxFixesPerHour)
	}

	// Check per-report attempts.
	if sg.attemptsByReport[reportID] >= sg.maxAttemptsPerReport {
		return false, fmt.Sprintf("max attempts reached for report %s (%d/%d)", reportID, sg.attemptsByReport[reportID], sg.maxAttemptsPerReport)
	}

	return true, ""
}

// RecordFix records a fix attempt.
func (sg *SafetyGuard) RecordFix(reportID string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.fixCount++
	sg.attemptsByReport[reportID]++
	sg.logger.Info("mesh.safety.fix_recorded",
		"report_id", reportID,
		"fixes_this_hour", sg.fixCount,
		"attempts_this_report", sg.attemptsByReport[reportID],
	)
}

// Reset clears all tracking state.
func (sg *SafetyGuard) Reset() {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.fixCount = 0
	sg.fixWindowStart = time.Now()
	sg.attemptsByReport = make(map[string]int)
}
