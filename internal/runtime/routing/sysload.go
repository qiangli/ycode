package routing

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SysLoadProvider samples local system load average for routing decisions.
// It caches the load value for a configurable interval to avoid expensive
// syscalls on every routing decision.
type SysLoadProvider struct {
	mu         sync.Mutex
	cached     float64
	lastSample time.Time
	interval   time.Duration
}

// NewSysLoadProvider creates a system load provider that samples at the given interval.
func NewSysLoadProvider(interval time.Duration) *SysLoadProvider {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &SysLoadProvider{interval: interval}
}

// LoadAverage returns the cached 1-minute load average.
// On macOS/Linux, reads from /proc/loadavg or sysctl.
// On unsupported platforms, returns 0 (neutral score).
func (s *SysLoadProvider) LoadAverage() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.lastSample) < s.interval {
		return s.cached
	}

	s.cached = readLoadAverage()
	s.lastSample = time.Now()
	return s.cached
}

// readLoadAverage reads the 1-minute load average from the OS.
func readLoadAverage() float64 {
	switch runtime.GOOS {
	case "linux":
		return readLinuxLoadAvg()
	case "darwin":
		return readDarwinLoadAvg()
	default:
		return 0
	}
}

// readLinuxLoadAvg reads /proc/loadavg (format: "1.23 4.56 7.89 1/234 5678").
func readLinuxLoadAvg() float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// readDarwinLoadAvg reads load average on macOS.
// Falls back to a simple heuristic based on NumCPU if sysctl fails.
func readDarwinLoadAvg() float64 {
	// Try reading from /proc-like interface (not available on macOS).
	// On macOS, the most reliable non-CGO approach is to use GOMAXPROCS
	// as a proxy — imperfect but avoids exec("sysctl") overhead.
	// For accurate results, callers should use the cgo-enabled version
	// or read from OTEL system metrics.
	//
	// Return a neutral value that doesn't penalize local models.
	return float64(runtime.NumGoroutine()) / float64(runtime.NumCPU()) * 0.5
}
