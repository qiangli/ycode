package agentpool

import (
	"sync"
	"time"
)

// LivenessState classifies agent health based on activity freshness.
// Inspired by agent-orchestrator's multi-source activity signal classification,
// gastown's heartbeat v2 (self-reported state), and paperclip's liveness
// classification service.
type LivenessState int

const (
	// LivenessHealthy means the agent produced output recently.
	LivenessHealthy LivenessState = iota
	// LivenessSuspicious means output has been silent longer than expected.
	LivenessSuspicious
	// LivenessCritical means the agent has been silent for an extended period.
	LivenessCritical
	// LivenessStranded means the agent appears abandoned.
	LivenessStranded
)

// String returns a human-readable liveness label.
func (s LivenessState) String() string {
	switch s {
	case LivenessHealthy:
		return "healthy"
	case LivenessSuspicious:
		return "suspicious"
	case LivenessCritical:
		return "critical"
	case LivenessStranded:
		return "stranded"
	default:
		return "unknown"
	}
}

// SelfReportedState is an advisory state reported by the agent itself.
// Unlike system-tracked AgentStatus, this reflects the agent's own assessment.
type SelfReportedState string

const (
	SelfReportWorking SelfReportedState = "working"
	SelfReportIdle    SelfReportedState = "idle"
	SelfReportBlocked SelfReportedState = "blocked"
	SelfReportExiting SelfReportedState = "exiting"
	SelfReportStuck   SelfReportedState = "stuck"
)

// LivenessThresholds configures the time-based liveness classification.
type LivenessThresholds struct {
	SuspiciousAfter time.Duration // default 5 min
	CriticalAfter   time.Duration // default 30 min
	StrandedAfter   time.Duration // default 2 hours
}

// DefaultThresholds returns sensible defaults for liveness classification.
func DefaultThresholds() LivenessThresholds {
	return LivenessThresholds{
		SuspiciousAfter: 5 * time.Minute,
		CriticalAfter:   30 * time.Minute,
		StrandedAfter:   2 * time.Hour,
	}
}

// LivenessClassifier monitors agent activity freshness and classifies health.
// Detection is decoupled from action: the classifier emits state, callers
// decide what to do (nudge, recover, escalate).
type LivenessClassifier struct {
	mu         sync.RWMutex
	thresholds LivenessThresholds

	// Per-agent tracking: last output timestamp and self-reported state.
	lastActivity   map[string]time.Time
	selfReported   map[string]SelfReportedState
	lastReportedAt map[string]time.Time
}

// NewLivenessClassifier creates a classifier with the given thresholds.
func NewLivenessClassifier(thresholds LivenessThresholds) *LivenessClassifier {
	return &LivenessClassifier{
		thresholds:     thresholds,
		lastActivity:   make(map[string]time.Time),
		selfReported:   make(map[string]SelfReportedState),
		lastReportedAt: make(map[string]time.Time),
	}
}

// RecordActivity notes that the agent produced output or used a tool.
func (lc *LivenessClassifier) RecordActivity(agentID string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.lastActivity[agentID] = time.Now()
}

// ReportState records an agent's self-reported state (heartbeat v2 pattern).
// The agent calls this to signal its own assessment of its health.
func (lc *LivenessClassifier) ReportState(agentID string, state SelfReportedState) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.selfReported[agentID] = state
	lc.lastReportedAt[agentID] = time.Now()
}

// Classify returns the liveness state for an agent. Classification uses a
// fallback chain: (1) check self-reported state, (2) check output freshness.
// Self-reported "stuck" always returns Critical regardless of freshness.
// Stale self-reports (older than StrandedAfter) are ignored.
func (lc *LivenessClassifier) Classify(agentID string) LivenessState {
	return lc.classifyAt(agentID, time.Now())
}

// classifyAt is the testable core that accepts a "now" timestamp.
func (lc *LivenessClassifier) classifyAt(agentID string, now time.Time) LivenessState {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	// Check self-reported state first (if fresh enough).
	if reportedAt, ok := lc.lastReportedAt[agentID]; ok {
		reportAge := now.Sub(reportedAt)
		if reportAge < lc.thresholds.StrandedAfter {
			switch lc.selfReported[agentID] {
			case SelfReportStuck:
				return LivenessCritical
			case SelfReportExiting:
				return LivenessHealthy // graceful exit is fine
			}
		}
	}

	// Fall back to output freshness.
	lastAct, ok := lc.lastActivity[agentID]
	if !ok {
		return LivenessStranded // never seen any activity
	}

	silence := now.Sub(lastAct)
	switch {
	case silence < lc.thresholds.SuspiciousAfter:
		return LivenessHealthy
	case silence < lc.thresholds.CriticalAfter:
		return LivenessSuspicious
	case silence < lc.thresholds.StrandedAfter:
		return LivenessCritical
	default:
		return LivenessStranded
	}
}

// Remove stops tracking an agent.
func (lc *LivenessClassifier) Remove(agentID string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	delete(lc.lastActivity, agentID)
	delete(lc.selfReported, agentID)
	delete(lc.lastReportedAt, agentID)
}

// ScanStale returns IDs of agents in Suspicious, Critical, or Stranded state.
// Useful for periodic patrol scans.
func (lc *LivenessClassifier) ScanStale(now time.Time) map[string]LivenessState {
	lc.mu.RLock()
	agents := make([]string, 0, len(lc.lastActivity))
	for id := range lc.lastActivity {
		agents = append(agents, id)
	}
	lc.mu.RUnlock()

	stale := make(map[string]LivenessState)
	for _, id := range agents {
		state := lc.classifyAt(id, now)
		if state > LivenessHealthy {
			stale[id] = state
		}
	}
	return stale
}
