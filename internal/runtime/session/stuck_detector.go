package session

import (
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// StuckDetectorConfig configures the stuck session detector.
type StuckDetectorConfig struct {
	// CheckInterval is how often to scan sessions (default 30s).
	CheckInterval time.Duration

	// StuckThreshold is how long a session must be in Processing/Waiting
	// before it is considered stuck (default 5 min).
	StuckThreshold time.Duration

	// Emitter for diagnostic events.
	Emitter *bus.DiagnosticEmitter
}

// StuckDetector periodically checks sessions for stuck states.
type StuckDetector struct {
	cfg      StuckDetectorConfig
	mu       sync.RWMutex
	trackers map[string]*LifecycleTracker
	stopCh   chan struct{}
	stopped  chan struct{}
}

// NewStuckDetector creates a stuck session detector.
func NewStuckDetector(cfg StuckDetectorConfig) *StuckDetector {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 30 * time.Second
	}
	if cfg.StuckThreshold <= 0 {
		cfg.StuckThreshold = 5 * time.Minute
	}
	return &StuckDetector{
		cfg:      cfg,
		trackers: make(map[string]*LifecycleTracker),
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Register adds a session lifecycle tracker to be monitored.
func (sd *StuckDetector) Register(sessionID string, tracker *LifecycleTracker) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	sd.trackers[sessionID] = tracker
}

// Unregister removes a session from monitoring.
func (sd *StuckDetector) Unregister(sessionID string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	delete(sd.trackers, sessionID)
}

// Start begins periodic stuck detection in a background goroutine.
func (sd *StuckDetector) Start() {
	go func() {
		defer close(sd.stopped)
		ticker := time.NewTicker(sd.cfg.CheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-sd.stopCh:
				return
			case <-ticker.C:
				sd.check()
			}
		}
	}()
}

// Stop shuts down the detector and waits for the goroutine to exit.
func (sd *StuckDetector) Stop() {
	close(sd.stopCh)
	<-sd.stopped
}

// check scans all registered sessions for stuck states.
func (sd *StuckDetector) check() {
	sd.mu.RLock()
	// Copy to avoid holding lock during emission.
	trackers := make(map[string]*LifecycleTracker, len(sd.trackers))
	for id, lt := range sd.trackers { //nolint:mapsloop // maps.Copy requires same type
		trackers[id] = lt
	}
	sd.mu.RUnlock()

	for sessionID, lt := range trackers {
		state := lt.State()
		if state != StateProcessing && state != StateWaiting {
			continue
		}

		age := lt.Duration()
		if age >= sd.cfg.StuckThreshold {
			if sd.cfg.Emitter != nil {
				sd.cfg.Emitter.EmitSessionStuck(sessionID, age, state.String())
			}
		}
	}
}

// StuckSessions returns sessions that are currently stuck.
// Useful for programmatic inspection without waiting for events.
func (sd *StuckDetector) StuckSessions() []StuckSessionInfo {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	var stuck []StuckSessionInfo
	for sessionID, lt := range sd.trackers {
		state := lt.State()
		if state != StateProcessing && state != StateWaiting {
			continue
		}
		age := lt.Duration()
		if age >= sd.cfg.StuckThreshold {
			stuck = append(stuck, StuckSessionInfo{
				SessionID: sessionID,
				State:     state,
				Duration:  age,
			})
		}
	}
	return stuck
}

// StuckSessionInfo describes a stuck session.
type StuckSessionInfo struct {
	SessionID string
	State     SessionState
	Duration  time.Duration
}
