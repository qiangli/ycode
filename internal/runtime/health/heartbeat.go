package health

import (
	"runtime"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// HeartbeatConfig configures the periodic heartbeat emitter.
type HeartbeatConfig struct {
	// Interval between heartbeats (default 30s).
	Interval time.Duration

	// Emitter for diagnostic events.
	Emitter *bus.DiagnosticEmitter
}

// SessionCounter returns the number of active sessions.
// Provided as a callback to avoid import cycles.
type SessionCounter func() int

// Heartbeat emits periodic health snapshots to the diagnostic bus.
type Heartbeat struct {
	cfg            HeartbeatConfig
	sessionCounter SessionCounter
	stopCh         chan struct{}
	stopped        chan struct{}

	// Custom gauges that consumers can register.
	mu     sync.RWMutex
	gauges map[string]func() any
}

// NewHeartbeat creates a heartbeat emitter.
func NewHeartbeat(cfg HeartbeatConfig, sessionCounter SessionCounter) *Heartbeat {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if sessionCounter == nil {
		sessionCounter = func() int { return 0 }
	}
	return &Heartbeat{
		cfg:            cfg,
		sessionCounter: sessionCounter,
		stopCh:         make(chan struct{}),
		stopped:        make(chan struct{}),
		gauges:         make(map[string]func() any),
	}
}

// RegisterGauge adds a named gauge that will be included in each heartbeat.
// The function is called at each heartbeat interval to get the current value.
func (h *Heartbeat) RegisterGauge(name string, fn func() any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gauges[name] = fn
}

// Start begins emitting heartbeats in a background goroutine.
func (h *Heartbeat) Start() {
	go func() {
		defer close(h.stopped)
		ticker := time.NewTicker(h.cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-h.stopCh:
				return
			case <-ticker.C:
				h.emit()
			}
		}
	}()
}

// Stop shuts down the heartbeat emitter and waits for it to finish.
func (h *Heartbeat) Stop() {
	close(h.stopCh)
	<-h.stopped
}

func (h *Heartbeat) emit() {
	if h.cfg.Emitter == nil {
		return
	}

	attrs := h.collectAttrs()
	activeSessions := h.sessionCounter()
	h.cfg.Emitter.EmitHeartbeat(activeSessions, attrs)
}

func (h *Heartbeat) collectAttrs() map[string]any {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	attrs := map[string]any{
		"goroutines":     runtime.NumGoroutine(),
		"heap_alloc_mb":  float64(memStats.HeapAlloc) / (1024 * 1024),
		"heap_sys_mb":    float64(memStats.HeapSys) / (1024 * 1024),
		"gc_pause_ns":    memStats.PauseNs[(memStats.NumGC+255)%256],
		"gc_num":         memStats.NumGC,
		"uptime_seconds": time.Since(startTime).Seconds(),
	}

	// Add custom gauges.
	h.mu.RLock()
	for name, fn := range h.gauges {
		attrs[name] = fn()
	}
	h.mu.RUnlock()

	return attrs
}

// startTime records when the process started (package init).
var startTime = time.Now()
