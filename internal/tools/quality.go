package tools

import (
	"log/slog"
	"sync"
	"time"
)

// ToolReliability holds computed reliability metrics for a tool.
type ToolReliability struct {
	Name          string
	TotalCalls    int
	SuccessCount  int
	FailureCount  int
	SuccessRate   float64 // 0.0-1.0
	AvgDurationMs float64
	LastFailure   time.Time
}

// QualityMonitor tracks tool reliability and detects degradation.
type QualityMonitor struct {
	mu        sync.Mutex
	stats     map[string]*toolStats
	threshold float64 // success rate below this triggers degradation (default 0.7)
	logger    *slog.Logger
}

type toolStats struct {
	calls     int
	successes int
	failures  int
	totalMs   float64
	lastFail  time.Time
}

// NewQualityMonitor creates a quality monitor.
func NewQualityMonitor(threshold float64) *QualityMonitor {
	if threshold <= 0 {
		threshold = 0.7
	}
	return &QualityMonitor{
		stats:     make(map[string]*toolStats),
		threshold: threshold,
		logger:    slog.Default(),
	}
}

// RecordCall records a tool invocation result.
func (qm *QualityMonitor) RecordCall(toolName string, success bool, durationMs float64) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	s, ok := qm.stats[toolName]
	if !ok {
		s = &toolStats{}
		qm.stats[toolName] = s
	}
	s.calls++
	s.totalMs += durationMs
	if success {
		s.successes++
	} else {
		s.failures++
		s.lastFail = time.Now()
	}
	qm.logger.Debug("tool.quality.record",
		"tool", toolName,
		"success", success,
		"duration_ms", durationMs,
		"total_calls", s.calls,
		"success_rate", float64(s.successes)/float64(s.calls),
	)
}

// Reliability returns the reliability metrics for a tool.
func (qm *QualityMonitor) Reliability(toolName string) *ToolReliability {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	s, ok := qm.stats[toolName]
	if !ok {
		return &ToolReliability{Name: toolName}
	}

	rate := 0.0
	if s.calls > 0 {
		rate = float64(s.successes) / float64(s.calls)
	}
	avgMs := 0.0
	if s.calls > 0 {
		avgMs = s.totalMs / float64(s.calls)
	}

	return &ToolReliability{
		Name:          toolName,
		TotalCalls:    s.calls,
		SuccessCount:  s.successes,
		FailureCount:  s.failures,
		SuccessRate:   rate,
		AvgDurationMs: avgMs,
		LastFailure:   s.lastFail,
	}
}

// DegradedTools returns tools whose success rate is below the threshold.
func (qm *QualityMonitor) DegradedTools() []ToolReliability {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	var degraded []ToolReliability
	for name, s := range qm.stats {
		if s.calls < 3 { // need minimum sample size
			continue
		}
		rate := float64(s.successes) / float64(s.calls)
		if rate < qm.threshold {
			degraded = append(degraded, ToolReliability{
				Name:         name,
				TotalCalls:   s.calls,
				SuccessCount: s.successes,
				FailureCount: s.failures,
				SuccessRate:  rate,
				LastFailure:  s.lastFail,
			})
		}
	}
	for _, d := range degraded {
		qm.logger.Warn("tool.quality.degraded",
			"tool", d.Name,
			"success_rate", d.SuccessRate,
			"total_calls", d.TotalCalls,
			"failures", d.FailureCount,
		)
	}
	return degraded
}

// Reset clears all statistics.
func (qm *QualityMonitor) Reset() {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.stats = make(map[string]*toolStats)
}
